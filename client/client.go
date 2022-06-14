package client

import (
	"bytes"
	"context"
	"encoding/gob"
	"fmt"
	"github.com/crystal-construct/shar/client/parser"
	"github.com/crystal-construct/shar/client/services"
	"github.com/crystal-construct/shar/internal/messages"
	"github.com/crystal-construct/shar/model"
	"github.com/crystal-construct/shar/telemetry/ctxutil"
	"github.com/golang/protobuf/ptypes/empty"
	"github.com/golang/protobuf/ptypes/wrappers"
	"github.com/nats-io/nats.go"
	tracesdk "go.opentelemetry.io/otel/sdk/trace"
	"go.uber.org/zap"
	"google.golang.org/protobuf/proto"
	"strconv"
	"strings"
	"time"
)

type serviceFn func(ctx context.Context, vars model.Vars) (model.Vars, error)

type Client struct {
	js       nats.JetStreamContext
	SvcTasks map[string]serviceFn
	storage  services.Storage
	log      *zap.Logger
	tp       *tracesdk.TracerProvider
	con      *nats.Conn
}

func New(log *zap.Logger) *Client {
	return &Client{
		SvcTasks: make(map[string]serviceFn),
		log:      log,
	}
}

func (c *Client) Dial(natsURL string) error {
	n, err := nats.Connect(natsURL)
	if err != nil {
		return fmt.Errorf("failed to connect to nats: %w", err)
	}
	js, err := n.JetStream()
	if err != nil {
		return fmt.Errorf("failed to connect to jetstream: %w", err)
	}
	c.js = js
	c.con = n
	c.storage, err = services.NewNatsClientProvider(c.log, js, nats.FileStorage)
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

func (c *Client) Listen(ctx context.Context) error {
	if err := c.listen(ctx); err != nil {
		return fmt.Errorf("failed to listen for user tasks: %w", err)
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
		return fmt.Errorf("failed to add consumer: %w", err)
	}
	sub, err := c.js.PullSubscribe(messages.WorkflowJobExecuteAll, "JobExecuteConsumer")
	if err != nil {
		return fmt.Errorf("failed to pull subscribe: %w", err)
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
			xctx := ctxutil.LoadContextFromNATSHeader(ctx, msg)
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
				if svcFn, ok := c.SvcTasks[job.Execute]; !ok {
					if err := msg.Ack(); err != nil {
						c.log.Error("failed to find service function", zap.Error(err), zap.String("fn", job.Execute))
					}
				} else {
					newVars, err := svcFn(xctx, c.decodeVars(job.Vars))
					if err != nil {
						if err := msg.Nak(); err != nil {
							c.log.Warn("nak failed", zap.Error(err), zap.String("subject", msg.Subject))
						}
						fmt.Println(err)
						continue
					}
					if err := c.CompleteServiceTask(xctx, ut.TrackingId, newVars); err != nil {
						if err := msg.Nak(); err != nil {
							c.log.Warn("nak failed", zap.Error(err), zap.String("subject", msg.Subject))
						}
						fmt.Println(err)
						continue
					}
					if err := msg.Ack(); err != nil {
						c.log.Error("service task ack failed", zap.Error(err), zap.String("subject", msg.Subject))
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

func (c *Client) encodeVars(vars map[string]interface{}) []byte {
	var buf bytes.Buffer
	enc := gob.NewEncoder(&buf)
	if err := enc.Encode(vars); err != nil {
		fmt.Println("failed to encode vars", zap.Any("vars", vars))
	}
	return buf.Bytes()
}

func (c *Client) decodeVars(vars []byte) model.Vars {
	ret := make(map[string]interface{})
	if vars == nil {
		return ret
	}
	r := bytes.NewReader(vars)
	d := gob.NewDecoder(r)
	if err := d.Decode(&ret); err != nil {
		fmt.Println(err)
	}
	return ret
}

func (c *Client) CompleteUserTask(ctx context.Context, jobId string, newVars model.Vars) error {
	job := &model.WorkflowState{TrackingId: jobId, Vars: c.encodeVars(newVars)}
	b, err := proto.Marshal(job)
	if err != nil {
		return fmt.Errorf("failed to marshal completed user task: %w", err)
	}
	msg := nats.NewMsg(messages.WorkflowJobUserTaskComplete)
	msg.Data = b
	ctxutil.LoadNATSHeaderFromContext(ctx, msg)
	_, err = c.js.PublishMsg(msg)
	if err != nil {
		return fmt.Errorf("failed to publish complete user task: %w", err)
	}
	return nil
}

func (c *Client) CompleteServiceTask(ctx context.Context, jobId string, newVars model.Vars) error {
	job := &model.WorkflowState{TrackingId: jobId, Vars: c.encodeVars(newVars)}
	b, err := proto.Marshal(job)
	if err != nil {
		return fmt.Errorf("failed to marshal complete service task: %w", err)
	}
	msg := nats.NewMsg(messages.WorkflowJobServiceTaskComplete)
	msg.Data = b
	ctxutil.LoadNATSHeaderFromContext(ctx, msg)
	_, err = c.js.PublishMsg(msg)
	if err != nil {
		return fmt.Errorf("failed to publish complete service task: %w", err)
	}
	return nil
}

func (c *Client) CompleteManualTask(ctx context.Context, jobId string, newVars model.Vars) error {
	job := &model.WorkflowState{TrackingId: jobId, Vars: c.encodeVars(newVars)}
	b, err := proto.Marshal(job)
	if err != nil {
		return fmt.Errorf("failed to marshal complete manual task: %w", err)
	}
	msg := nats.NewMsg(messages.WorkflowJobManualTaskComplete)
	msg.Data = b
	ctxutil.LoadNATSHeaderFromContext(ctx, msg)
	_, err = c.js.PublishMsg(msg)
	if err != nil {
		return fmt.Errorf("failed to publish complete manual task: %w", err)
	}
	return nil
}

func (c *Client) LoadBMPNWorkflowFromBytes(ctx context.Context, b []byte) (string, error) {
	rdr := bytes.NewReader(b)
	if wf, err := parser.Parse(rdr); err == nil {
		res := &wrappers.StringValue{}
		if err := callAPI(ctx, c.con, messages.ApiStoreWorkflow, wf, res); err != nil {
			return "", fmt.Errorf("failed to store workflow: %w", err)
		}
		return res.Value, nil
	} else {
		return "", fmt.Errorf("failed to parse workflow: %w", err)
	}

}

func (c *Client) CancelWorkflowInstance(ctx context.Context, instanceID string) error {
	res := &empty.Empty{}
	req := &model.CancelWorkflowInstanceRequest{Id: instanceID}
	if err := callAPI(ctx, c.con, messages.ApiCancelWorkflowInstance, req, res); err != nil {
		return fmt.Errorf("failed to cancel workflow instance: %w", err)
	}
	return nil
}

func (c *Client) LaunchWorkflow(ctx context.Context, workflowName string, vars model.Vars) (string, error) {
	req := &model.LaunchWorkflowRequest{Name: workflowName, Vars: c.encodeVars(vars)}
	res := &wrappers.StringValue{}
	if err := callAPI(ctx, c.con, messages.ApiLaunchWorkflow, req, res); err != nil {
		return "", fmt.Errorf("failed to launch workflow: %w", err)
	}
	return res.Value, nil
}

func (c *Client) ListWorkflowInstance(ctx context.Context, name string) ([]*model.ListWorkflowInstanceResult, error) {
	req := &model.ListWorkflowInstanceRequest{WorkflowName: name}
	res := &model.ListWorkflowInstanceResponse{}
	if err := callAPI(ctx, c.con, messages.ApiListWorkflowInstance, req, res); err != nil {
		return nil, fmt.Errorf("failed to launch workflow: %w", err)
	}
	return res.Result, nil
}

func (c *Client) ListWorkflows(ctx context.Context) ([]*model.ListWorkflowResult, error) {
	req := &empty.Empty{}
	res := &model.ListWorkflowsResponse{}
	if err := callAPI(ctx, c.con, messages.ApiListWorkflows, req, res); err != nil {
		return nil, fmt.Errorf("failed to launch workflow: %w", err)
	}
	return res.Result, nil
}

func (c *Client) GetWorkflowInstanceStatus(ctx context.Context, id string) ([]*model.WorkflowState, error) {
	req := &model.GetWorkflowInstanceStatusRequest{Id: id}
	res := &model.WorkflowInstanceStatus{}
	if err := callAPI(ctx, c.con, messages.ApiGetWorkflowStatus, req, res); err != nil {
		return nil, fmt.Errorf("failed to launch workflow: %w", err)
	}
	return res.State, nil
}

func (c *Client) SendMessage(ctx context.Context, name string, key string) interface{} {
	req := &model.SendMessageRequest{Name: name, Key: key}
	res := &empty.Empty{}
	if err := callAPI(ctx, c.con, messages.ApiSendMessage, req, res); err != nil {
		return fmt.Errorf("failed to send message: %w", err)
	}
	return nil
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
