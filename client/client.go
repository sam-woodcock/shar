package client

import (
	"bytes"
	"context"
	"fmt"
	"github.com/golang/protobuf/ptypes/wrappers"
	"github.com/nats-io/nats.go"
	"gitlab.com/shar-workflow/shar/client/parser"
	"gitlab.com/shar-workflow/shar/client/services"
	"gitlab.com/shar-workflow/shar/model"
	"gitlab.com/shar-workflow/shar/server/errors"
	"gitlab.com/shar-workflow/shar/server/messages"
	"gitlab.com/shar-workflow/shar/server/vars"
	"go.uber.org/zap"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/emptypb"
	"google.golang.org/protobuf/types/known/wrapperspb"
	"strconv"
	"strings"
	"time"
)

type Command struct {
	cl    *Client
	wfiID string
}

func (c *Command) SendMessage(ctx context.Context, name string, key any) error {
	return c.cl.SendMessage(ctx, c.wfiID, name, key)
}

type serviceFn func(ctx context.Context, vars model.Vars) (model.Vars, error)
type senderFn func(ctx context.Context, client *Command, vars model.Vars) error

type Client struct {
	js          nats.JetStreamContext
	SvcTasks    map[string]serviceFn
	storage     services.Storage
	log         *zap.Logger
	con         *nats.Conn
	complete    chan *model.WorkflowInstanceComplete
	MsgSender   map[string]senderFn
	storageType nats.StorageType
}

type ClientOption interface {
	configure(client *Client)
}

type EphemeralStorage struct {
	ClientOption
}

func (o EphemeralStorage) configure(client *Client) {
	client.storageType = nats.MemoryStorage
}

func New(log *zap.Logger, option ...ClientOption) *Client {
	client := &Client{
		storageType: nats.FileStorage,
		SvcTasks:    make(map[string]serviceFn),
		MsgSender:   make(map[string]senderFn),
		log:         log,
	}
	for _, i := range option {
		i.configure(client)
	}
	return client
}

func (c *Client) Dial(natsURL string) error {
	n, err := nats.Connect(natsURL)
	if err != nil {
		return c.clientErr(context.Background(), "failed to connect to nats", err)
	}
	js, err := n.JetStream()
	if err != nil {
		return c.clientErr(context.Background(), "failed to connect to jetstream: %w", err)
	}
	c.js = js
	c.con = n
	c.storage, err = services.NewNatsClientProvider(c.log, js, c.storageType)
	if err != nil {
		return err
	}
	return nil
}

func (c *Client) RegisterServiceTask(taskName string, fn serviceFn) {
	if _, ok := c.SvcTasks[taskName]; ok {
		c.log.Fatal("Service task " + taskName + " already registered")
	}
	c.SvcTasks[taskName] = fn
}

func (c *Client) RegisterMessageSender(messageName string, sender senderFn) {
	if _, ok := c.MsgSender[messageName]; ok {
		c.log.Fatal("message sender " + messageName + " already registered")
	}
	c.MsgSender[messageName] = sender
}

func (c *Client) Listen(ctx context.Context) error {
	if err := c.listen(ctx); err != nil {
		return c.clientErr(ctx, "failed to listen for user tasks: %w", err)
	}
	if err := c.listenWorkflowComplete(ctx); err != nil {
		return c.clientErr(ctx, "failed to listen for workflow complete: %w", err)
	}
	return nil
}

func (c *Client) listen(ctx context.Context) error {
	if _, err := c.js.AddConsumer("WORKFLOW", &nats.ConsumerConfig{
		Durable:       "JobExecuteConsumer",
		Description:   "",
		FilterSubject: messages.WorkflowJobExecuteAll,
		AckPolicy:     nats.AckExplicitPolicy,
	}); err != nil {
		return c.clientErr(ctx, "failed to add consumer: %w", err)
	}
	sub, err := c.js.PullSubscribe(messages.WorkflowJobExecuteAll, "JobExecuteConsumer")
	if err != nil {
		return c.clientErr(ctx, "failed to pull subscribe: %w", err)
	}
	go func() {
		for {
			var msg *nats.Msg
			if msgs, err := sub.Fetch(1, nats.MaxWait(5*time.Minute)); err != nil {
				fmt.Println(err)
				continue
			} else {
				msg = msgs[0]
			}
			xctx := context.Background()
			ut := &model.WorkflowState{}
			if err := proto.Unmarshal(msg.Data, ut); err != nil {
				fmt.Println(err)
			}
			switch ut.ElementType {
			case "serviceTask":
				job, err := c.storage.GetJob(xctx, ut.TrackingId)
				if err != nil {
					c.log.Error("failed to get job", zap.Error(err), zap.String("JobId", ut.TrackingId))
					continue
				}
				if svcFn, ok := c.SvcTasks[*job.Execute]; !ok {
					if err := msg.Ack(); err != nil {
						c.log.Error("failed to find service function", zap.Error(err), zap.String("fn", *job.Execute))
					}
				} else {
					dv, err := vars.Decode(c.log, job.Vars)
					if err != nil {
						c.log.Error("failed to decode vars", zap.Error(err), zap.String("fn", *job.Execute))
					}
					newVars, err := svcFn(xctx, dv)
					if err != nil {
						if err := msg.Nak(); err != nil {
							c.log.Warn("nak failed", zap.Error(err), zap.String("subject", msg.Subject))
						}
						c.log.Warn("failed during execution of service task function", zap.Error(err))
						continue
					}
					if err := c.CompleteServiceTask(xctx, ut.TrackingId, newVars); err != nil {
						if err := msg.Nak(); err != nil {
							c.log.Warn("nak failed", zap.Error(err), zap.String("subject", msg.Subject))
						}
						c.log.Warn("failed to complete service task", zap.Error(err))
						continue
					}
					if err := msg.Ack(); err != nil {
						c.log.Error("service task ack failed", zap.Error(err), zap.String("subject", msg.Subject))
					}
				}
			case "intermediateThrowEvent":
				job, err := c.storage.GetJob(xctx, ut.TrackingId)
				if err != nil {
					c.log.Error("failed to get send message task", zap.Error(err), zap.String("JobId", ut.TrackingId))
					continue
				}
				if sendFn, ok := c.MsgSender[*job.Execute]; !ok {
					if err := msg.Ack(); err != nil {
						c.log.Error("failed to find send message function", zap.Error(err), zap.String("fn", *job.Execute))
					}
				} else {
					dv, err := vars.Decode(c.log, job.Vars)
					if err != nil {
						c.log.Error("failed to decode vars", zap.Error(err), zap.String("fn", *job.Execute))
					}
					if err := sendFn(xctx, &Command{cl: c, wfiID: job.WorkflowInstanceId}, dv); err != nil {
						if err := msg.Nak(); err != nil {
							c.log.Warn("nak failed", zap.Error(err), zap.String("subject", msg.Subject))
						}
						c.log.Warn("nats listener", zap.Error(err))
						continue
					}
					if err := c.CompleteServiceTask(xctx, ut.TrackingId, make(map[string]any)); err != nil {
						if err := msg.Nak(); err != nil {
							c.log.Warn("nak failed", zap.Error(err), zap.String("subject", msg.Subject))
						}
						c.log.Error("proto unmarshal error", zap.Error(err))
						continue
					}
					if err := msg.Ack(); err != nil {
						c.log.Error("send message ack failed", zap.Error(err), zap.String("subject", msg.Subject))
					}
				}
			default:
				if err := msg.Ack(); err != nil {
					c.log.Error("default ack failed", zap.Error(err), zap.String("subject", msg.Subject))
				}
			}
		}
	}()
	return nil
}

func (c *Client) listenWorkflowComplete(ctx context.Context) error {
	_, err := c.con.Subscribe(messages.WorkflowInstanceComplete, func(msg *nats.Msg) {
		st := &model.WorkflowState{}
		if err := proto.Unmarshal(msg.Data, st); err != nil {
			c.log.Error("proto unmarshal error", zap.Error(err))
		}

		if c.complete != nil {
			c.complete <- &model.WorkflowInstanceComplete{
				WorkflowInstanceId: st.WorkflowInstanceId,
				WorkflowId:         st.WorkflowId,
			}
			if err := msg.Ack(); err != nil {
				c.log.Error("send message ack failed", zap.Error(err), zap.String("subject", msg.Subject))
			}
		}
	})
	if err != nil {
		return err
	}
	return nil
}

func (c *Client) CompleteUserTask(ctx context.Context, jobId string, newVars model.Vars) error {
	vb, err := vars.Encode(c.log, newVars)
	job := &model.WorkflowState{TrackingId: jobId, Vars: vb}
	b, err := proto.Marshal(job)
	if err != nil {
		return c.clientErr(ctx, "failed to marshal completed user task: %w", err)
	}
	msg := nats.NewMsg(messages.WorkflowJobUserTaskComplete)
	msg.Data = b
	_, err = c.js.PublishMsg(msg)
	if err != nil {
		return c.clientErr(ctx, "failed to publish complete user task: %w", err)
	}
	return nil
}

func (c *Client) CompleteServiceTask(ctx context.Context, jobId string, newVars model.Vars) error {
	ev, err := vars.Encode(c.log, newVars)
	if err != nil {
		return err
	}
	job := &model.WorkflowState{TrackingId: jobId, Vars: ev}
	b, err := proto.Marshal(job)
	if err != nil {
		return c.clientErr(ctx, "failed to marshal complete service task: %w", err)
	}
	msg := nats.NewMsg(messages.WorkflowJobServiceTaskComplete)
	msg.Data = b
	_, err = c.js.PublishMsg(msg)
	if err != nil {
		return c.clientErr(ctx, "failed to publish complete service task: %w", err)
	}
	return nil
}

func (c *Client) CompleteManualTask(ctx context.Context, jobId string, newVars model.Vars) error {
	ev, err := vars.Encode(c.log, newVars)
	if err != nil {
		return err
	}
	job := &model.WorkflowState{TrackingId: jobId, Vars: ev}
	b, err := proto.Marshal(job)
	if err != nil {
		return c.clientErr(ctx, "failed to marshal complete manual task: %w", err)
	}
	msg := nats.NewMsg(messages.WorkflowJobManualTaskComplete)
	msg.Data = b
	_, err = c.js.PublishMsg(msg)
	if err != nil {
		return c.clientErr(ctx, "failed to publish complete manual task: %w", err)
	}
	return nil
}

func (c *Client) LoadBPMNWorkflowFromBytes(ctx context.Context, name string, b []byte) (string, error) {
	rdr := bytes.NewReader(b)
	if wf, err := parser.Parse(name, rdr); err == nil {
		res := &wrapperspb.StringValue{}
		if err := callAPI(ctx, c.con, messages.ApiStoreWorkflow, wf, res); err != nil {
			return "", c.clientErr(ctx, "failed to store workflow: %w", err)
		}
		return res.Value, nil
	} else {
		return "", c.clientErr(ctx, "failed to parse workflow: %w", err)
	}

}

func (c *Client) CancelWorkflowInstance(ctx context.Context, instanceID string) error {
	res := &emptypb.Empty{}
	req := &model.CancelWorkflowInstanceRequest{Id: instanceID}
	if err := callAPI(ctx, c.con, messages.ApiCancelWorkflowInstance, req, res); err != nil {
		return c.clientErr(ctx, "failed to cancel workflow instance: %w", err)
	}
	return nil
}

func (c *Client) LaunchWorkflow(ctx context.Context, workflowName string, mvars model.Vars) (string, error) {
	ev, err := vars.Encode(c.log, mvars)
	if err != nil {
		return "", err
	}
	req := &model.LaunchWorkflowRequest{Name: workflowName, Vars: ev}
	res := &wrappers.StringValue{}
	if err := callAPI(ctx, c.con, messages.ApiLaunchWorkflow, req, res); err != nil {
		return "", c.clientErr(ctx, "failed to launch workflow: %w", err)
	}
	return res.Value, nil
}

func (c *Client) ListWorkflowInstance(ctx context.Context, name string) ([]*model.ListWorkflowInstanceResult, error) {
	req := &model.ListWorkflowInstanceRequest{WorkflowName: name}
	res := &model.ListWorkflowInstanceResponse{}
	if err := callAPI(ctx, c.con, messages.ApiListWorkflowInstance, req, res); err != nil {
		return nil, c.clientErr(ctx, "failed to launch workflow: %w", err)
	}
	return res.Result, nil
}

func (c *Client) ListWorkflows(ctx context.Context) ([]*model.ListWorkflowResult, error) {
	req := &emptypb.Empty{}
	res := &model.ListWorkflowsResponse{}
	if err := callAPI(ctx, c.con, messages.ApiListWorkflows, req, res); err != nil {
		return nil, c.clientErr(ctx, "failed to launch workflow: %w", err)
	}
	return res.Result, nil
}

func (c *Client) GetWorkflowInstanceStatus(ctx context.Context, id string) ([]*model.WorkflowState, error) {
	req := &model.GetWorkflowInstanceStatusRequest{Id: id}
	res := &model.WorkflowInstanceStatus{}
	if err := callAPI(ctx, c.con, messages.ApiGetWorkflowStatus, req, res); err != nil {
		return nil, c.clientErr(ctx, "failed to launch workflow: %w", err)
	}
	return res.State, nil
}

func (c *Client) SendMessage(ctx context.Context, workflowInstanceID string, name string, key any) error {
	var skey string
	switch key.(type) {
	case string:
		skey = "\"" + fmt.Sprintf("%+v", key) + "\""
	default:
		skey = fmt.Sprintf("%+v", key)
	}
	req := &model.SendMessageRequest{Name: name, Key: skey, WorkflowInstanceId: workflowInstanceID}
	res := &emptypb.Empty{}
	if err := callAPI(ctx, c.con, messages.ApiSendMessage, req, res); err != nil {
		return c.clientErr(ctx, "failed to send message: %w", err)
	}
	return nil
}

func (c *Client) clientErr(ctx context.Context, msg string, err error, z ...zap.Field) error {
	z = append(z, zap.Error(err))
	c.log.Error(msg, z...)
	return err
}

func (c *Client) fatalClientErr(ctx context.Context, msg string, err error, z ...zap.Field) error {
	err2 := c.clientErr(ctx, msg, err, z...)
	return errors.NewErrWorkflowFatal(msg, err2)
}

func (c *Client) RegisterWorkflowInstanceComplete(fn chan *model.WorkflowInstanceComplete) {
	c.complete = fn
}

func callAPI[T proto.Message, U proto.Message](ctx context.Context, con *nats.Conn, subject string, command T, ret U) error {
	b, err := proto.Marshal(command)
	if err != nil {
		return err
	}
	msg := nats.NewMsg(subject)
	msg.Data = b
	res, err := con.Request(subject, b, time.Second*30)
	if err != nil {
		return err
	}
	if len(res.Data) > 4 && string(res.Data[0:4]) == "ERR_" {
		em := strings.Split(string(res.Data), "_")
		e := strings.Split(em[1], "|")
		i, err := strconv.Atoi(e[0])
		if err != nil {
			i = 0
		}
		return &ApiError{Code: i, Message: e[1]}
	}
	if err := proto.Unmarshal(res.Data, ret); err != nil {
		return err
	}
	return nil
}
