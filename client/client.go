package client

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"github.com/golang/protobuf/ptypes/wrappers"
	"github.com/nats-io/nats.go"
	"gitlab.com/shar-workflow/shar/client/parser"
	"gitlab.com/shar-workflow/shar/client/services"
	"gitlab.com/shar-workflow/shar/common"
	"gitlab.com/shar-workflow/shar/common/ctxkey"
	"gitlab.com/shar-workflow/shar/common/subj"
	"gitlab.com/shar-workflow/shar/common/workflow"
	"gitlab.com/shar-workflow/shar/model"
	errors2 "gitlab.com/shar-workflow/shar/server/errors"
	"gitlab.com/shar-workflow/shar/server/messages"
	"gitlab.com/shar-workflow/shar/server/vars"
	"go.uber.org/zap"
	"google.golang.org/grpc/codes"
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

func (c *Command) SendMessage(ctx context.Context, name string, key any, vars model.Vars) error {
	return c.cl.SendMessage(ctx, c.wfiID, name, key, vars)
}

type serviceFn func(ctx context.Context, vars model.Vars) (model.Vars, error)
type senderFn func(ctx context.Context, client *Command, vars model.Vars) error

type Client struct {
	js             nats.JetStreamContext
	SvcTasks       map[string]serviceFn
	storage        services.Storage
	log            *zap.Logger
	con            *nats.Conn
	complete       chan *model.WorkflowInstanceComplete
	MsgSender      map[string]senderFn
	storageType    nats.StorageType
	ns             string
	listenTasks    map[string]struct{}
	msgListenTasks map[string]struct{}
	txJS           nats.JetStreamContext
	txCon          *nats.Conn
}

type Option interface {
	configure(client *Client)
}

func New(log *zap.Logger, option ...Option) *Client {
	client := &Client{
		storageType:    nats.FileStorage,
		SvcTasks:       make(map[string]serviceFn),
		MsgSender:      make(map[string]senderFn),
		listenTasks:    make(map[string]struct{}),
		msgListenTasks: make(map[string]struct{}),
		log:            log,
		ns:             "default",
	}
	for _, i := range option {
		i.configure(client)
	}
	return client
}

func (c *Client) Dial(natsURL string, opts ...nats.Option) error {
	n, err := nats.Connect(natsURL, opts...)
	if err != nil {
		return c.clientErr(context.Background(), err)
	}
	txnc, err := nats.Connect(natsURL, opts...)
	if err != nil {
		return c.clientErr(context.Background(), err)
	}
	js, err := n.JetStream()
	if err != nil {
		return c.clientErr(context.Background(), err)
	}
	txJS, err := txnc.JetStream()
	if err != nil {
		return c.clientErr(context.Background(), err)
	}
	if err != nil {
		return c.clientErr(context.Background(), err)
	}
	c.js = js
	c.txJS = txJS
	c.con = n
	c.txCon = txnc
	c.storage, err = services.NewNatsClientProvider(c.log, js, c.storageType)
	if err != nil {
		return err
	}
	return nil
}

func (c *Client) RegisterServiceTask(ctx context.Context, taskName string, fn serviceFn) error {
	id, err := c.getServiceTaskRoutingID(ctx, taskName)
	if err != nil {
		return err
	}
	if _, ok := c.SvcTasks[taskName]; ok {
		c.log.Fatal("Service task " + taskName + " already registered")
	}
	c.SvcTasks[taskName] = fn
	c.listenTasks[id] = struct{}{}
	return nil
}

func (c *Client) RegisterMessageSender(ctx context.Context, workflowName string, messageName string, sender senderFn) error {
	id, err := c.getMessageSenderRoutingID(ctx, workflowName, messageName)
	if err != nil {
		return err
	}
	if _, ok := c.MsgSender[messageName]; ok {
		c.log.Fatal("message sender " + messageName + " already registered")
	}
	c.MsgSender[messageName] = sender
	c.msgListenTasks[id] = struct{}{}
	return nil
}

func (c *Client) Listen(ctx context.Context) error {
	if err := c.listen(ctx); err != nil {
		return c.clientErr(ctx, err)
	}
	if err := c.listenWorkflowComplete(ctx); err != nil {
		return c.clientErr(ctx, err)
	}
	return nil
}

func (c *Client) listen(ctx context.Context) error {
	closer := make(chan struct{}, 1)
	tasks := make(map[string]string)
	for i := range c.listenTasks {
		tasks[i] = subj.NS(messages.WorkflowJobServiceTaskExecute+"."+i, c.ns)
	}
	for i := range c.msgListenTasks {
		tasks[i] = subj.NS(messages.WorkflowJobSendMessageExecute+"."+i, c.ns)
	}
	for k, v := range tasks {
		common.Process(ctx, c.js, c.log, "jobExecute", closer, v, "ServiceTask_"+k, 200, func(ctx context.Context, msg *nats.Msg) (bool, error) {
			xctx := context.Background()

			ut := &model.WorkflowState{}
			if err := proto.Unmarshal(msg.Data, ut); err != nil {
				c.log.Error("error unmarshaling", zap.Error(err))
				return false, err
			}
			xctx = context.WithValue(xctx, ctxkey.WorkflowInstanceID, ut.WorkflowInstanceId)
			switch ut.ElementType {
			case "serviceTask":
				job, err := c.storage.GetJob(xctx, common.TrackingID(ut.Id).ID())
				if err != nil {
					c.log.Error("failed to get job", zap.Error(err), zap.String("JobId", common.TrackingID(ut.Id).ID()))
					return false, err
				}
				if svcFn, ok := c.SvcTasks[*job.Execute]; !ok {
					c.log.Error("failed to find service function", zap.Error(err), zap.String("fn", *job.Execute))
					return false, err
				} else {
					dv, err := vars.Decode(c.log, job.Vars)
					if err != nil {
						c.log.Error("failed to decode vars", zap.Error(err), zap.String("fn", *job.Execute))
						return false, err
					}
					newVars, err := func() (v model.Vars, e error) {
						defer func() {
							if r := recover(); r != nil {
								v = model.Vars{}
								e = &errors2.ErrWorkflowFatal{Err: fmt.Errorf("call to service task \"%s\" terminated in panic: %w", *ut.Execute, r.(error))}
							}
						}()
						v, e = svcFn(xctx, dv)
						return
					}()
					if err != nil {
						var handled bool
						wfe := &workflow.Error{}
						if errors.As(err, wfe) {
							res := &model.HandleWorkflowErrorResponse{}
							req := &model.HandleWorkflowErrorRequest{TrackingId: common.TrackingID(ut.Id).ID(), ErrorCode: wfe.Code}
							if err2 := callAPI(ctx, c.txCon, messages.ApiHandleWorkflowError, req, res); err2 != nil {
								c.log.Error("failed to handle workflow error", zap.Any("workflowError", wfe), zap.Error(err2))
								return true, err
							}
							handled = res.Handled
						}
						if !handled {
							c.log.Warn("failed during execution of service task function", zap.Error(err))
						}
						return wfe.Code != "", err
					}
					err = c.completeServiceTask(xctx, common.TrackingID(ut.Id).ID(), newVars)
					ae := &apiError{}
					if errors.As(err, &ae) {
						if codes.Code(ae.Code) == codes.Internal {
							c.log.Error("failed to complete service task", zap.Error(err))
							e := &model.Error{
								Id:   "",
								Name: ae.Message,
								Code: "client-" + strconv.Itoa(ae.Code),
							}
							if err := c.cancelWorkflowInstanceWithError(ctx, ut.WorkflowInstanceId, e); err != nil {
								c.log.Error("failed to cancel workflow instance in response to fatal error")
							}
							return true, nil
						}
					} else if errors2.IsWorkflowFatal(err) {
						return true, err
					}
					if err != nil {
						c.log.Warn("failed to complete service task", zap.Error(err))
						return false, err
					}
					return true, nil
				}
			case "intermediateThrowEvent":
				job, err := c.storage.GetJob(xctx, common.TrackingID(ut.Id).ID())
				if err != nil {
					c.log.Error("failed to get send message task", zap.Error(err), zap.String("JobId", common.TrackingID(ut.Id).ID()))
					return false, err
				}
				if sendFn, ok := c.MsgSender[*job.Execute]; !ok {
					return true, nil
				} else {
					dv, err := vars.Decode(c.log, job.Vars)
					if err != nil {
						c.log.Error("failed to decode vars", zap.Error(err), zap.String("fn", *job.Execute))
						return false, err
					}
					if err := sendFn(xctx, &Command{cl: c, wfiID: job.WorkflowInstanceId}, dv); err != nil {
						c.log.Warn("nats listener", zap.Error(err))
						return false, err
					}
					if err := c.completeSendMessage(xctx, common.TrackingID(ut.Id).ID(), make(map[string]any)); err != nil {
						c.log.Error("proto unmarshal error", zap.Error(err))
						return false, err
					}
					return true, nil
				}
			}
			return true, nil
		})
	}
	return nil
}

func (c *Client) listenWorkflowComplete(_ context.Context) error {
	_, err := c.con.Subscribe(subj.NS(messages.WorkflowInstanceTerminated, c.ns), func(msg *nats.Msg) {
		st := &model.WorkflowState{}
		if err := proto.Unmarshal(msg.Data, st); err != nil {
			c.log.Error("proto unmarshal error", zap.Error(err))
		}

		if c.complete != nil {
			c.complete <- &model.WorkflowInstanceComplete{
				WorkflowInstanceId: st.WorkflowInstanceId,
				WorkflowId:         st.WorkflowId,
				WorkflowState:      st.State,
				Error:              st.Error,
			}
		}
	})
	if err != nil {
		return err
	}
	return nil
}

func (c *Client) ListUserTaskIDs(ctx context.Context, owner string) (*model.UserTasks, error) {
	res := &model.UserTasks{}
	req := &model.ListUserTasksRequest{Owner: owner}
	if err := callAPI(ctx, c.txCon, messages.ApiListUserTaskIDs, req, res); err != nil {
		return nil, c.clientErr(ctx, err)
	}
	return res, nil
}

func (c *Client) CompleteUserTask(ctx context.Context, owner string, trackingID string, newVars model.Vars) error {
	ev, err := vars.Encode(c.log, newVars)
	if err != nil {
		return err
	}
	res := &emptypb.Empty{}
	req := &model.CompleteUserTaskRequest{Owner: owner, TrackingId: trackingID, Vars: ev}
	if err := callAPI(ctx, c.txCon, messages.ApiCompleteUserTask, req, res); err != nil {
		return c.clientErr(ctx, err)
	}
	return nil
}

func (c *Client) completeServiceTask(ctx context.Context, trackingID string, newVars model.Vars) error {
	ev, err := vars.Encode(c.log, newVars)
	if err != nil {
		return err
	}
	res := &emptypb.Empty{}
	req := &model.CompleteServiceTaskRequest{TrackingId: trackingID, Vars: ev}
	if err := callAPI(ctx, c.txCon, messages.ApiCompleteServiceTask, req, res); err != nil {
		return c.clientErr(ctx, err)
	}
	return nil
}

func (c *Client) completeSendMessage(ctx context.Context, trackingID string, newVars model.Vars) error {
	ev, err := vars.Encode(c.log, newVars)
	if err != nil {
		return err
	}
	res := &emptypb.Empty{}
	req := &model.CompleteSendMessageRequest{TrackingId: trackingID, Vars: ev}
	if err := callAPI(ctx, c.txCon, messages.ApiCompleteSendMessage, req, res); err != nil {
		return c.clientErr(ctx, err)
	}
	return nil
}

func (c *Client) LoadBPMNWorkflowFromBytes(ctx context.Context, name string, b []byte) (string, error) {
	rdr := bytes.NewReader(b)
	if wf, err := parser.Parse(name, rdr); err == nil {
		res := &wrapperspb.StringValue{}
		if err := callAPI(ctx, c.txCon, messages.ApiStoreWorkflow, wf, res); err != nil {
			return "", c.clientErr(ctx, err)
		}
		return res.Value, nil
	} else {
		return "", c.clientErr(ctx, err)
	}

}

func (c *Client) CancelWorkflowInstance(ctx context.Context, instanceID string) error {
	return c.cancelWorkflowInstanceWithError(ctx, instanceID, nil)
}

func (c *Client) cancelWorkflowInstanceWithError(ctx context.Context, instanceID string, wfe *model.Error) error {
	res := &emptypb.Empty{}
	req := &model.CancelWorkflowInstanceRequest{
		Id:    instanceID,
		State: model.CancellationState_Errored,
		Error: wfe,
	}
	if err := callAPI(ctx, c.txCon, messages.ApiCancelWorkflowInstance, req, res); err != nil {
		return c.clientErr(ctx, err)
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
	if err := callAPI(ctx, c.txCon, messages.ApiLaunchWorkflow, req, res); err != nil {
		return "", c.clientErr(ctx, err)
	}
	return res.Value, nil
}

func (c *Client) ListWorkflowInstance(ctx context.Context, name string) ([]*model.ListWorkflowInstanceResult, error) {
	req := &model.ListWorkflowInstanceRequest{WorkflowName: name}
	res := &model.ListWorkflowInstanceResponse{}
	if err := callAPI(ctx, c.txCon, messages.ApiListWorkflowInstance, req, res); err != nil {
		return nil, c.clientErr(ctx, err)
	}
	return res.Result, nil
}

func (c *Client) ListWorkflows(ctx context.Context) ([]*model.ListWorkflowResult, error) {
	req := &emptypb.Empty{}
	res := &model.ListWorkflowsResponse{}
	if err := callAPI(ctx, c.txCon, messages.ApiListWorkflows, req, res); err != nil {
		return nil, c.clientErr(ctx, err)
	}
	return res.Result, nil
}

func (c *Client) GetWorkflowInstanceStatus(ctx context.Context, id string) ([]*model.WorkflowState, error) {
	req := &model.GetWorkflowInstanceStatusRequest{Id: id}
	res := &model.WorkflowInstanceStatus{}
	if err := callAPI(ctx, c.txCon, messages.ApiGetWorkflowStatus, req, res); err != nil {
		return nil, c.clientErr(ctx, err)
	}
	return res.State, nil
}

func (c *Client) getServiceTaskRoutingID(ctx context.Context, id string) (string, error) {
	req := &wrappers.StringValue{Value: id}
	res := &wrappers.StringValue{}
	if err := callAPI(ctx, c.txCon, messages.ApiGetServiceTaskRoutingID, req, res); err != nil {
		return "", c.clientErr(ctx, err)
	}
	return res.Value, nil
}

func (c *Client) getMessageSenderRoutingID(ctx context.Context, workflowName string, messageName string) (string, error) {
	req := &model.GetMessageSenderRoutingIdRequest{WorkflowName: workflowName, MessageName: messageName}
	res := &wrappers.StringValue{}
	if err := callAPI(ctx, c.txCon, messages.ApiGetMessageSenderRoutingID, req, res); err != nil {
		return "", c.clientErr(ctx, err)
	}
	return res.Value, nil
}

func (c *Client) GetUserTask(ctx context.Context, owner string, trackingID string) (*model.GetUserTaskResponse, model.Vars, error) {
	req := &model.GetUserTaskRequest{Owner: owner, TrackingId: trackingID}
	res := &model.GetUserTaskResponse{}
	if err := callAPI(ctx, c.txCon, messages.ApiGetUserTask, req, res); err != nil {
		return nil, nil, c.clientErr(ctx, err)
	}
	v, err := vars.Decode(c.log, res.Vars)
	if err != nil {
		return nil, nil, c.clientErr(ctx, err)
	}
	return res, v, nil
}

func (c *Client) SendMessage(ctx context.Context, workflowInstanceID string, name string, key any, mvars model.Vars) error {
	if workflowInstanceID == "" {
		workflowInstanceID = ctx.Value(ctxkey.WorkflowInstanceID).(string)
	}
	var skey string
	switch key.(type) {
	case string:
		skey = "\"" + fmt.Sprintf("%+v", key) + "\""
	default:
		skey = fmt.Sprintf("%+v", key)
	}
	b, err := vars.Encode(c.log, mvars)
	if err != nil {
		return err
	}
	req := &model.SendMessageRequest{Name: name, Key: skey, WorkflowInstanceId: workflowInstanceID, Vars: b}
	res := &emptypb.Empty{}
	if err := callAPI(ctx, c.txCon, messages.ApiSendMessage, req, res); err != nil {
		return c.clientErr(ctx, err)
	}
	return nil
}

func (c *Client) clientErr(_ context.Context, err error) error {
	return err
}

func (c *Client) RegisterWorkflowInstanceComplete(fn chan *model.WorkflowInstanceComplete) {
	c.complete = fn
}

func (c *Client) GetServerInstanceStats(ctx context.Context) (*model.WorkflowStats, error) {
	req := &emptypb.Empty{}
	res := &model.WorkflowStats{}
	if err := callAPI(ctx, c.txCon, messages.ApiGetServerInstanceStats, req, res); err != nil {
		return nil, c.clientErr(ctx, err)
	}
	return res, nil
}

func callAPI[T proto.Message, U proto.Message](_ context.Context, con *nats.Conn, subject string, command T, ret U) error {
	b, err := proto.Marshal(command)
	if err != nil {
		return err
	}
	msg := nats.NewMsg(subject)
	msg.Data = b
	res, err := con.Request(subject, b, time.Second*60)
	if err != nil {
		if err == nats.ErrNoResponders {
			err = fmt.Errorf("shar-client: shar server is offline or missing from the current nats server")
		}
		return err
	}
	if len(res.Data) > 4 && string(res.Data[0:4]) == "ERR_" {
		em := strings.Split(string(res.Data), "_")
		e := strings.Split(em[1], "|")
		i, err := strconv.Atoi(e[0])
		if err != nil {
			i = 0
		}
		ae := &apiError{Code: i, Message: e[1]}
		if codes.Code(i) == codes.Internal {
			return &errors2.ErrWorkflowFatal{Err: ae}
		}
		return ae
	}
	if err := proto.Unmarshal(res.Data, ret); err != nil {
		return err
	}
	return nil
}
