package client

import (
	"bytes"
	"compress/gzip"
	"context"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"

	"github.com/golang/protobuf/ptypes/wrappers"
	"github.com/nats-io/nats.go"
	"github.com/segmentio/ksuid"
	"gitlab.com/shar-workflow/shar/client/parser"
	"gitlab.com/shar-workflow/shar/common"
	"gitlab.com/shar-workflow/shar/common/ctxkey"
	"gitlab.com/shar-workflow/shar/common/header"
	"gitlab.com/shar-workflow/shar/common/logx"
	"gitlab.com/shar-workflow/shar/common/subj"
	"gitlab.com/shar-workflow/shar/common/workflow"
	"gitlab.com/shar-workflow/shar/model"
	errors2 "gitlab.com/shar-workflow/shar/server/errors"
	"gitlab.com/shar-workflow/shar/server/messages"
	"gitlab.com/shar-workflow/shar/server/vars"
	"golang.org/x/exp/slog"
	"google.golang.org/grpc/codes"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/emptypb"
	"google.golang.org/protobuf/types/known/wrapperspb"
)

// LogClient represents a client which is capable of logging to the SHAR infrastructure.
type LogClient interface {
	// Log logs to the underlying SHAR infrastructure.
	Log(ctx context.Context, severity messages.WorkflowLogLevel, code int32, message string, attrs map[string]string) error
}

// JobClient represents a client that is sent to all service tasks to facilitate logging.
type JobClient interface {
	LogClient
}

type jobClient struct {
	cl         *Client
	trackingID string
}

// Log logs to the span related to this jobClient instance.
func (c *jobClient) Log(ctx context.Context, severity messages.WorkflowLogLevel, code int32, message string, attrs map[string]string) error {
	return c.cl.clientLog(ctx, c.trackingID, severity, code, message, attrs)
}

// MessageClient represents a client which supports logging and sending Workflow Messages to the underlying SHAR instrastructure.
type MessageClient interface {
	LogClient
	// SendMessage sends a Workflow Message
	SendMessage(ctx context.Context, name string, key any, vars model.Vars) error
}

type messageClient struct {
	cl         *Client
	wfiID      string
	trackingID string
}

// SendMessage sends a Workflow Message into the SHAR engine
func (c *messageClient) SendMessage(ctx context.Context, name string, key any, vars model.Vars) error {
	return c.cl.SendMessage(ctx, c.wfiID, name, key, vars)
}

// Log logs to the span related to this jobClient instance.
func (c *messageClient) Log(ctx context.Context, severity messages.WorkflowLogLevel, code int32, message string, attrs map[string]string) error {
	return c.cl.clientLog(ctx, c.trackingID, severity, code, message, attrs)
}

// ServiceFn provides the signature for service task functions.
type ServiceFn func(ctx context.Context, client JobClient, vars model.Vars) (model.Vars, workflow.WrappedError)

// SenderFn provides the signature for functions that can act as Workflow Message senders.
type SenderFn func(ctx context.Context, client MessageClient, vars model.Vars) error

// Client implements a SHAR client capable of listening for service task activations, listening for Workflow Messages, and interating with the API
type Client struct {
	js             nats.JetStreamContext
	SvcTasks       map[string]ServiceFn
	con            *nats.Conn
	complete       chan *model.WorkflowInstanceComplete
	MsgSender      map[string]SenderFn
	storageType    nats.StorageType
	ns             string
	listenTasks    map[string]struct{}
	msgListenTasks map[string]struct{}
	txJS           nats.JetStreamContext
	txCon          *nats.Conn
	wfInstance     nats.KeyValue
	wf             nats.KeyValue
	job            nats.KeyValue
	concurrency    int
}

// Option represents a configuration changer for the client.
type Option interface {
	configure(client *Client)
}

// New creates a new SHAR client instance
func New(option ...Option) *Client {
	client := &Client{
		storageType:    nats.FileStorage,
		SvcTasks:       make(map[string]ServiceFn),
		MsgSender:      make(map[string]SenderFn),
		listenTasks:    make(map[string]struct{}),
		msgListenTasks: make(map[string]struct{}),
		ns:             "default",
		concurrency:    10,
	}
	for _, i := range option {
		i.configure(client)
	}
	return client
}

// Dial instructs the client to connect to a NATS server.
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
	c.js = js
	c.txJS = txJS
	c.con = n
	c.txCon = txnc

	if c.wfInstance, err = js.KeyValue("WORKFLOW_INSTANCE"); err != nil {
		return fmt.Errorf("failed to connect to workflow instance kv: %w", err)
	}
	if c.wf, err = js.KeyValue("WORKFLOW_DEF"); err != nil {
		return fmt.Errorf("failed to connect to workflow definition kv: %w", err)
	}
	if c.job, err = js.KeyValue("WORKFLOW_JOB"); err != nil {
		return fmt.Errorf("failed to connect to workflow job kv: %w", err)
	}
	return nil
}

// RegisterServiceTask adds a new service task to listen for to the client.
func (c *Client) RegisterServiceTask(ctx context.Context, taskName string, fn ServiceFn) error {
	id, err := c.getServiceTaskRoutingID(ctx, taskName)
	if err != nil {
		return fmt.Errorf("failed to get service task routing: %w", err)
	}
	if _, ok := c.SvcTasks[taskName]; ok {
		return fmt.Errorf("service task '%s' already registered: %w", taskName, errors2.ErrServiceTaskAlreadyRegistered)
	}
	c.SvcTasks[taskName] = fn
	c.listenTasks[id] = struct{}{}
	return nil
}

// RegisterMessageSender registers a function that requires support for sending Workflow Messages
func (c *Client) RegisterMessageSender(ctx context.Context, workflowName string, messageName string, sender SenderFn) error {
	id, err := c.getMessageSenderRoutingID(ctx, workflowName, messageName)
	if err != nil {
		return fmt.Errorf("failed to get message sender routing: %w", err)
	}
	if _, ok := c.MsgSender[messageName]; ok {
		return fmt.Errorf("message sender '%s' already registered: %w", messageName, errors2.ErrMessageSenderAlreadyRegistered)
	}
	c.MsgSender[messageName] = sender
	c.msgListenTasks[id] = struct{}{}
	return nil
}

// Listen starts processing the client message queues.
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
		err := common.Process(ctx, c.js, "jobExecute", closer, v, "ServiceTask_"+k, c.concurrency, func(ctx context.Context, log *slog.Logger, msg *nats.Msg) (bool, error) {
			ut := &model.WorkflowState{}
			if err := proto.Unmarshal(msg.Data, ut); err != nil {
				log.Error("error unmarshaling", err)
				return false, fmt.Errorf("failed during service task listener: %w", err)
			}
			ctx = context.WithValue(ctx, ctxkey.WorkflowInstanceID, ut.WorkflowInstanceId)
			val, err := header.FromMsg(ctx, msg)
			if err != nil {
				return true, &errors2.ErrWorkflowFatal{Err: fmt.Errorf("failed to obtain headers from message: %w", err)}
			}
			ctx = header.ToCtx(ctx, val)
			switch ut.ElementType {
			case "serviceTask":
				trackingID := common.TrackingID(ut.Id).ID()
				job, err := c.GetJob(ctx, trackingID)
				if err != nil {
					log.Error("failed to get job", err, slog.String("JobId", trackingID))
					return false, fmt.Errorf("failed to get service task job kv: %w", err)
				}
				svcFn, ok := c.SvcTasks[*job.Execute]
				if !ok {
					log.Error("failed to find service function", err, slog.String("fn", *job.Execute))
					return false, fmt.Errorf("failed to find service task function: %w", err)
				}
				dv, err := vars.Decode(ctx, job.Vars)
				if err != nil {
					log.Error("failed to decode vars", err, slog.String("fn", *job.Execute))
					return false, fmt.Errorf("failed to decode service task job variables: %w", err)
				}
				newVars, err := func() (v model.Vars, e error) {
					defer func() {
						if r := recover(); r != nil {
							v = model.Vars{}
							e = &errors2.ErrWorkflowFatal{Err: fmt.Errorf("call to service task \"%s\" terminated in panic: %w", *ut.Execute, r.(error))}
						}
					}()
					v, e = svcFn(ctx, &jobClient{cl: c, trackingID: trackingID}, dv)
					return
				}()
				if err != nil {
					var handled bool
					wfe := &workflow.Error{}
					if errors.As(err, wfe) {
						v, err := vars.Encode(ctx, newVars)
						if err != nil {
							return true, &errors2.ErrWorkflowFatal{Err: fmt.Errorf("failed to encode service task variables: %w", err)}
						}
						res := &model.HandleWorkflowErrorResponse{}
						req := &model.HandleWorkflowErrorRequest{TrackingId: trackingID, ErrorCode: wfe.Code, Vars: v}
						if err2 := callAPI(ctx, c.txCon, messages.APIHandleWorkflowError, req, res); err2 != nil {
							// TODO: This isn't right.  If this call fails it assumes it is handled!
							reterr := fmt.Errorf("failed to handle workflow error: %w", err2)
							return true, logx.Err(ctx, "failed to handle a workflow error", reterr, slog.Any("workflowError", wfe))
						}
						handled = res.Handled
					}
					if !handled {
						log.Warn("failed during execution of service task function", err)
					}
					return wfe.Code != "", err
				}
				err = c.completeServiceTask(ctx, trackingID, newVars)
				ae := &apiError{}
				if errors.As(err, &ae) {
					if codes.Code(ae.Code) == codes.Internal {
						log.Error("failed to complete service task", err)
						e := &model.Error{
							Id:   "",
							Name: ae.Message,
							Code: "client-" + strconv.Itoa(ae.Code),
						}
						if err := c.cancelWorkflowInstanceWithError(ctx, ut.WorkflowInstanceId, e); err != nil {
							log.Error("failed to cancel workflow instance in response to fatal error", err)
						}
						return true, nil
					}
				} else if errors2.IsWorkflowFatal(err) {
					return true, err
				}
				if err != nil {
					log.Warn("failed to complete service task", err)
					return false, fmt.Errorf("failed to complete service task: %w", err)
				}
				return true, nil

			case "intermediateThrowEvent":
				trackingID := common.TrackingID(ut.Id).ID()
				job, err := c.GetJob(ctx, trackingID)
				if err != nil {
					log.Error("failed to get send message task", err, slog.String("JobId", common.TrackingID(ut.Id).ID()))
					return false, fmt.Errorf("failed to complete send message task: %w", err)
				}
				sendFn, ok := c.MsgSender[*job.Execute]
				if !ok {
					return true, nil
				}

				dv, err := vars.Decode(ctx, job.Vars)
				if err != nil {
					log.Error("failed to decode vars", err, slog.String("fn", *job.Execute))
					return false, &errors2.ErrWorkflowFatal{Err: fmt.Errorf("failed to decode send message variables: %w", err)}
				}
				ctx = context.WithValue(ctx, ctxkey.TrackingID, trackingID)
				if err := sendFn(ctx, &messageClient{cl: c, trackingID: trackingID, wfiID: job.WorkflowInstanceId}, dv); err != nil {
					log.Warn("nats listener", err)
					return false, err
				}
				if err := c.completeSendMessage(ctx, trackingID, make(map[string]any)); errors2.IsWorkflowFatal(err) {
					log.Error("a fatal error occurred in message sender "+*job.Execute, err)
				} else if err != nil {
					log.Error("API error", err)
					return false, err
				}
				return true, nil
			}
			return true, nil
		})
		if err != nil {
			return fmt.Errorf("failed to connect to service task consumer: %w", err)
		}
	}
	return nil
}

func (c *Client) listenWorkflowComplete(ctx context.Context) error {
	log := slog.FromContext(ctx)
	_, err := c.con.Subscribe(subj.NS(messages.WorkflowInstanceTerminated, c.ns), func(msg *nats.Msg) {
		st := &model.WorkflowState{}
		if err := proto.Unmarshal(msg.Data, st); err != nil {
			log.Error("proto unmarshal error", err)
			return
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
		return fmt.Errorf("failed to subscribe to workflow termination: %w", err)
	}
	return nil
}

// ListUserTaskIDs returns a list of user tasks for a particular owner
func (c *Client) ListUserTaskIDs(ctx context.Context, owner string) (*model.UserTasks, error) {
	res := &model.UserTasks{}
	req := &model.ListUserTasksRequest{Owner: owner}
	if err := callAPI(ctx, c.txCon, messages.APIListUserTaskIDs, req, res); err != nil {
		return nil, c.clientErr(ctx, err)
	}
	return res, nil
}

// CompleteUserTask completes a task and sends the variables back to the workflow
func (c *Client) CompleteUserTask(ctx context.Context, owner string, trackingID string, newVars model.Vars) error {
	ev, err := vars.Encode(ctx, newVars)
	if err != nil {
		return fmt.Errorf("failed to decode variables for complete user task: %w", err)
	}
	res := &emptypb.Empty{}
	req := &model.CompleteUserTaskRequest{Owner: owner, TrackingId: trackingID, Vars: ev}
	if err := callAPI(ctx, c.txCon, messages.APICompleteUserTask, req, res); err != nil {
		return c.clientErr(ctx, err)
	}
	return nil
}

func (c *Client) completeServiceTask(ctx context.Context, trackingID string, newVars model.Vars) error {
	ev, err := vars.Encode(ctx, newVars)
	if err != nil {
		return fmt.Errorf("failed to decode variables for complete service task: %w", err)
	}
	res := &emptypb.Empty{}
	req := &model.CompleteServiceTaskRequest{TrackingId: trackingID, Vars: ev}
	if err := callAPI(ctx, c.txCon, messages.APICompleteServiceTask, req, res); err != nil {
		return c.clientErr(ctx, err)
	}
	return nil
}

func (c *Client) completeSendMessage(ctx context.Context, trackingID string, newVars model.Vars) error {
	ev, err := vars.Encode(ctx, newVars)
	if err != nil {
		return fmt.Errorf("failed to decode variables for complete send message: %w", err)
	}
	res := &emptypb.Empty{}
	req := &model.CompleteSendMessageRequest{TrackingId: trackingID, Vars: ev}
	if err := callAPI(ctx, c.txCon, messages.APICompleteSendMessageTask, req, res); err != nil {
		return c.clientErr(ctx, err)
	}
	return nil
}

// LoadBPMNWorkflowFromBytes loads, parses, and stores a BPMN workflow in SHAR.
func (c *Client) LoadBPMNWorkflowFromBytes(ctx context.Context, name string, b []byte) (string, error) {
	rdr := bytes.NewReader(b)
	wf, err := parser.Parse(name, rdr)
	if err != nil {
		return "", c.clientErr(ctx, err)
	}
	rdr = bytes.NewReader(b)
	compressed := &bytes.Buffer{}
	archiver := gzip.NewWriter(compressed)
	if _, err := io.Copy(archiver, rdr); err != nil {
		return "", fmt.Errorf("fasiled to compress source: %w", err)
	}
	if err := archiver.Close(); err != nil {
		return "", fmt.Errorf("fasiled to complete source compression: %w", err)
	}
	wf.GzipSource = compressed.Bytes()

	res := &wrapperspb.StringValue{}
	if err := callAPI(ctx, c.txCon, messages.APIStoreWorkflow, wf, res); err != nil {
		return "", c.clientErr(ctx, err)
	}
	return res.Value, nil
}

// HasWorkflowDefinitionChanged - given a workflow name and a BPMN xml, return true if the resulting definition is different.
func (c *Client) HasWorkflowDefinitionChanged(ctx context.Context, name string, b []byte) (bool, error) {
	versions, err := c.GetWorkflowVersions(ctx, name)
	if err != nil {
		if strings.Contains(err.Error(), "not found") {
			return true, nil
		}
		return false, err
	}
	rdr := bytes.NewReader(b)
	wf, err := parser.Parse(name, rdr)
	if err != nil {
		return false, c.clientErr(ctx, err)
	}
	hash, err := workflow.GetHash(wf)
	if err != nil {
		return false, c.clientErr(ctx, err)
	}
	return !bytes.Equal(versions.Version[len(versions.Version)-1].Sha256, hash), nil
}

// GetWorkflowVersions - returns a list of versions for a given workflow.
func (c *Client) GetWorkflowVersions(ctx context.Context, name string) (*model.WorkflowVersions, error) {
	req := &model.GetWorkflowVersionsRequest{
		Name: name,
	}
	res := &model.GetWorkflowVersionsResponse{}
	if err := callAPI(ctx, c.txCon, messages.APIGetWorkflowVersions, req, res); err != nil {
		return nil, c.clientErr(ctx, err)
	}
	return res.Versions, nil
}

// GetWorkflow - retrieves a workflow model given its ID
func (c *Client) GetWorkflow(ctx context.Context, id string) (*model.Workflow, error) {
	req := &model.GetWorkflowRequest{
		Id: id,
	}
	res := &model.GetWorkflowResponse{}
	if err := callAPI(ctx, c.txCon, messages.APIGetWorkflow, req, res); err != nil {
		return nil, c.clientErr(ctx, err)
	}
	return res.Definition, nil
}

// CancelWorkflowInstance cancels a running workflow instance.
func (c *Client) CancelWorkflowInstance(ctx context.Context, instanceID string) error {
	return c.cancelWorkflowInstanceWithError(ctx, instanceID, nil)
}

func (c *Client) cancelWorkflowInstanceWithError(ctx context.Context, instanceID string, wfe *model.Error) error {
	res := &emptypb.Empty{}
	req := &model.CancelWorkflowInstanceRequest{
		Id:    instanceID,
		State: model.CancellationState_errored,
		Error: wfe,
	}
	if err := callAPI(ctx, c.txCon, messages.APICancelWorkflowInstance, req, res); err != nil {
		return c.clientErr(ctx, err)
	}
	return nil
}

// LaunchWorkflow launches a new workflow instance.
func (c *Client) LaunchWorkflow(ctx context.Context, workflowName string, mvars model.Vars) (string, string, error) {
	ev, err := vars.Encode(ctx, mvars)
	if err != nil {
		return "", "", fmt.Errorf("failed to encode variables for launch workflow: %w", err)
	}
	req := &model.LaunchWorkflowRequest{Name: workflowName, Vars: ev}
	res := &model.LaunchWorkflowResponse{}
	if err := callAPI(ctx, c.txCon, messages.APILaunchWorkflow, req, res); err != nil {
		return "", "", c.clientErr(ctx, err)
	}
	return res.InstanceId, res.WorkflowId, nil
}

// ListWorkflowInstance gets a list of running workflow instances by workflow name.
func (c *Client) ListWorkflowInstance(ctx context.Context, name string) ([]*model.ListWorkflowInstanceResult, error) {
	req := &model.ListWorkflowInstanceRequest{WorkflowName: name}
	res := &model.ListWorkflowInstanceResponse{}
	if err := callAPI(ctx, c.txCon, messages.APIListWorkflowInstance, req, res); err != nil {
		return nil, c.clientErr(ctx, err)
	}
	return res.Result, nil
}

// ListWorkflows gets a list of launchable workflow in SHAR.
func (c *Client) ListWorkflows(ctx context.Context) ([]*model.ListWorkflowResult, error) {
	req := &emptypb.Empty{}
	res := &model.ListWorkflowsResponse{}
	if err := callAPI(ctx, c.txCon, messages.APIListWorkflows, req, res); err != nil {
		return nil, c.clientErr(ctx, err)
	}
	return res.Result, nil
}

// GetWorkflowInstanceStatus get the current execution state for a workflow instance.
func (c *Client) GetWorkflowInstanceStatus(ctx context.Context, id string) ([]*model.WorkflowState, error) {
	req := &model.GetWorkflowInstanceStatusRequest{Id: id}
	res := &model.WorkflowInstanceStatus{}
	if err := callAPI(ctx, c.txCon, messages.APIGetWorkflowStatus, req, res); err != nil {
		return nil, c.clientErr(ctx, err)
	}
	return res.State, nil
}

func (c *Client) getServiceTaskRoutingID(ctx context.Context, id string) (string, error) {
	req := &wrappers.StringValue{Value: id}
	res := &wrappers.StringValue{}
	if err := callAPI(ctx, c.txCon, messages.APIGetServiceTaskRoutingID, req, res); err != nil {
		return "", c.clientErr(ctx, err)
	}
	return res.Value, nil
}

func (c *Client) getMessageSenderRoutingID(ctx context.Context, workflowName string, messageName string) (string, error) {
	req := &model.GetMessageSenderRoutingIdRequest{WorkflowName: workflowName, MessageName: messageName}
	res := &wrappers.StringValue{}
	if err := callAPI(ctx, c.txCon, messages.APIGetMessageSenderRoutingID, req, res); err != nil {
		return "", c.clientErr(ctx, err)
	}
	return res.Value, nil
}

// GetUserTask fetches details for a user task based upon an ID obtained from, ListUserTasks
func (c *Client) GetUserTask(ctx context.Context, owner string, trackingID string) (*model.GetUserTaskResponse, model.Vars, error) {
	req := &model.GetUserTaskRequest{Owner: owner, TrackingId: trackingID}
	res := &model.GetUserTaskResponse{}
	if err := callAPI(ctx, c.txCon, messages.APIGetUserTask, req, res); err != nil {
		return nil, nil, c.clientErr(ctx, err)
	}
	v, err := vars.Decode(ctx, res.Vars)
	if err != nil {
		return nil, nil, c.clientErr(ctx, err)
	}
	return res, v, nil
}

// SendMessage sends a Workflow Message to a specific workflow instance
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
	b, err := vars.Encode(ctx, mvars)
	if err != nil {
		return fmt.Errorf("failed to encode variables for send message: %w", err)
	}
	req := &model.SendMessageRequest{Name: name, Key: skey, WorkflowInstanceId: workflowInstanceID, Vars: b}
	res := &emptypb.Empty{}
	if err := callAPI(ctx, c.txCon, messages.APISendMessage, req, res); err != nil {
		return c.clientErr(ctx, err)
	}
	return nil
}

func (c *Client) clientErr(_ context.Context, err error) error {
	return fmt.Errorf("client error: %w", err)
}

// RegisterWorkflowInstanceComplete registers a function to run when workflow instances complete.
func (c *Client) RegisterWorkflowInstanceComplete(fn chan *model.WorkflowInstanceComplete) {
	c.complete = fn
}

// GetServerInstanceStats is an unsupported function to obtain server metrics
func (c *Client) GetServerInstanceStats(ctx context.Context) (*model.WorkflowStats, error) {
	req := &emptypb.Empty{}
	res := &model.WorkflowStats{}
	if err := callAPI(ctx, c.txCon, messages.APIGetServerInstanceStats, req, res); err != nil {
		return nil, c.clientErr(ctx, err)
	}
	return res, nil
}

func (c *Client) clientLog(ctx context.Context, trackingID string, severity messages.WorkflowLogLevel, code int32, message string, attrs map[string]string) error {
	if err := common.Log(ctx, c.txJS, trackingID, model.LogSource_logSourceJob, severity, code, message, attrs); err != nil {
		return fmt.Errorf("failed to client log: %w", err)
	}
	return nil
}

// GetJob returns a Job given a tracking ID
func (c *Client) GetJob(ctx context.Context, id string) (*model.WorkflowState, error) {
	job := &model.WorkflowState{}
	// TODO: Stop direct data read
	if err := common.LoadObj(ctx, c.job, id, job); err != nil {
		return nil, fmt.Errorf("failed load object for get job: %w", err)
	}
	return job, nil
}

func callAPI[T proto.Message, U proto.Message](ctx context.Context, con *nats.Conn, subject string, command T, ret U) error {
	val := header.FromCtx(ctx)
	b, err := proto.Marshal(command)
	if err != nil {
		return fmt.Errorf("failed to marshal proto for call API: %w", err)
	}
	msg := nats.NewMsg(subject)
	err = header.ToMsg(ctx, val, msg)
	if err != nil {
		return fmt.Errorf("failed to attach headers to outgoing API message: %w", err)
	}
	msg.Header.Set("cid", ksuid.New().String())
	msg.Data = b
	res, err := con.RequestMsg(msg, time.Second*60)
	if err != nil {
		if errors.Is(err, nats.ErrNoResponders) {
			err = fmt.Errorf("shar-client: shar server is offline or missing from the current nats server")
		}
		return fmt.Errorf("failed during API call: %w", err)
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
		return fmt.Errorf("failed to unmarshal proto for call API: %w", err)
	}
	return nil
}
