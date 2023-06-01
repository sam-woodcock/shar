package client

import (
	"bytes"
	"compress/gzip"
	"context"
	"errors"
	"fmt"
	"github.com/hashicorp/go-version"
	"github.com/nats-io/nats.go"
	"gitlab.com/shar-workflow/shar/client/api"
	"gitlab.com/shar-workflow/shar/client/parser"
	"gitlab.com/shar-workflow/shar/common"
	"gitlab.com/shar-workflow/shar/common/ctxkey"
	"gitlab.com/shar-workflow/shar/common/element"
	"gitlab.com/shar-workflow/shar/common/header"
	"gitlab.com/shar-workflow/shar/common/logx"
	"gitlab.com/shar-workflow/shar/common/setup"
	"gitlab.com/shar-workflow/shar/common/setup/upgrader"
	"gitlab.com/shar-workflow/shar/common/subj"
	version2 "gitlab.com/shar-workflow/shar/common/version"
	"gitlab.com/shar-workflow/shar/common/workflow"
	api2 "gitlab.com/shar-workflow/shar/internal/client/api"
	"gitlab.com/shar-workflow/shar/model"
	errors2 "gitlab.com/shar-workflow/shar/server/errors"
	"gitlab.com/shar-workflow/shar/server/errors/keys"
	"gitlab.com/shar-workflow/shar/server/messages"
	"gitlab.com/shar-workflow/shar/server/vars"
	"golang.org/x/exp/slog"
	"google.golang.org/grpc/codes"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/emptypb"
	"google.golang.org/protobuf/types/known/wrapperspb"
	"io"
	"strconv"
	"strings"
	"sync"
	"time"
)

const internalProcessInstanceId = "__INTERNAL_PIID"

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
	return c.cl.SendMessage(ctx, name, key, vars)
}

// Log logs to the span related to this jobClient instance.
func (c *messageClient) Log(ctx context.Context, severity messages.WorkflowLogLevel, code int32, message string, attrs map[string]string) error {
	return c.cl.clientLog(ctx, c.trackingID, severity, code, message, attrs)
}

// ServiceFn provides the signature for service task functions.
type ServiceFn func(ctx context.Context, client JobClient, vars model.Vars) (model.Vars, error)

// ProcessTerminateFn provides the signature for process terminate functions.
type ProcessTerminateFn func(ctx context.Context, vars model.Vars, wfError *model.Error, endState model.CancellationState)

// SenderFn provides the signature for functions that can act as Workflow Message senders.
type SenderFn func(ctx context.Context, client MessageClient, vars model.Vars) error

// Client implements a SHAR client capable of listening for service task activations, listening for Workflow Messages, and interating with the API
type Client struct {
	js                              nats.JetStreamContext
	SvcTasks                        map[string]ServiceFn
	con                             *nats.Conn
	MsgSender                       map[string]SenderFn
	storageType                     nats.StorageType
	ns                              string
	listenTasks                     map[string]struct{}
	msgListenTasks                  map[string]struct{}
	proCompleteTasks                map[string]ProcessTerminateFn
	txJS                            nats.JetStreamContext
	txCon                           *nats.Conn
	wfInstance                      nats.KeyValue
	wf                              nats.KeyValue
	job                             nats.KeyValue
	concurrency                     int
	ExpectedCompatibleServerVersion *version.Version
	ExpectedServerVersion           *version.Version
	version                         *version.Version
}

// Option represents a configuration changer for the client.
type Option interface {
	configure(client *Client)
}

// New creates a new SHAR client instance
func New(option ...Option) *Client {
	ver, err := version.NewVersion(version2.Version)
	if err != nil {
		panic(err)
	}
	client := &Client{
		storageType:      nats.FileStorage,
		SvcTasks:         make(map[string]ServiceFn),
		MsgSender:        make(map[string]SenderFn),
		listenTasks:      make(map[string]struct{}),
		msgListenTasks:   make(map[string]struct{}),
		proCompleteTasks: make(map[string]ProcessTerminateFn),
		ns:               "default",
		concurrency:      10,
		version:          ver,
	}
	for _, i := range option {
		i.configure(client)
	}
	return client
}

// Dial instructs the client to connect to a NATS server.
func (c *Client) Dial(ctx context.Context, natsURL string, opts ...nats.Option) error {
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

	_, err = c.GetServerVersion(ctx)
	if err != nil {
		return fmt.Errorf("server version: %w", err)
	}

	if c.wfInstance, err = js.KeyValue("WORKFLOW_INSTANCE"); err != nil {
		return fmt.Errorf("connect to workflow instance kv: %w", err)
	}
	if c.wf, err = js.KeyValue("WORKFLOW_DEF"); err != nil {
		return fmt.Errorf("connect to workflow definition kv: %w", err)
	}
	if c.job, err = js.KeyValue("WORKFLOW_JOB"); err != nil {
		return fmt.Errorf("connect to workflow job kv: %w", err)
	}

	cdef := &nats.ConsumerConfig{
		Durable:         "ProcessTerminateConsumer_" + c.ns,
		Description:     "Processing queue for process end",
		AckPolicy:       nats.AckExplicitPolicy,
		AckWait:         30 * time.Second,
		FilterSubject:   subj.NS(messages.WorkflowProcessTerminated, c.ns),
		MaxAckPending:   65535,
		MaxRequestBatch: 1,
	}
	if err := setup.EnsureConsumer(js, "WORKFLOW", *cdef, false); err != nil {
		return fmt.Errorf("setting up end event queue")
	}

	return nil
}

// RegisterServiceTask adds a new service task to listen for to the client.
func (c *Client) RegisterServiceTask(ctx context.Context, taskName string, fn ServiceFn, opts ...RegOpt) error {
	opt := &registerTaskOptions{}
	for _, o := range opts {
		o.apply(opt)
	}
	id, err := c.getServiceTaskRoutingID(ctx, taskName)
	if err != nil {
		return fmt.Errorf("get service task routing: %w", err)
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
	if _, ok := c.MsgSender[workflowName+"_"+messageName]; ok {
		return fmt.Errorf("message sender '%s' already registered: %w", messageName, errors2.ErrMessageSenderAlreadyRegistered)
	}
	c.MsgSender[workflowName+"_"+messageName] = sender
	c.msgListenTasks[workflowName+"_"+messageName] = struct{}{}
	return nil
}

// Listen starts processing the client message queues.
func (c *Client) Listen(ctx context.Context) error {
	if err := c.listen(ctx); err != nil {
		return c.clientErr(ctx, err)
	}
	if err := c.listenProcessTerminate(ctx); err != nil {
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
		cName := "ServiceTask_" + k
		cInf, err := c.js.ConsumerInfo("WORKFLOW", cName)
		if err != nil {
			return fmt.Errorf("listen obtaining consumer info for %s: %w", cName, err)
		}
		ackTimeout := cInf.Config.AckWait
		err = common.Process(ctx, c.js, "jobExecute", closer, v, cName, c.concurrency, func(ctx context.Context, log *slog.Logger, msg *nats.Msg) (bool, error) {
			// Check version compatibility of incoming call.
			sharCompat := msg.Header.Get(header.NatsCompatHeader)
			if sharCompat != "" {
				sVer, err := version.NewVersion(sharCompat)
				if err != nil {
					return false, fmt.Errorf("compatibility issue: shar server version corrupt %s: %w", sVer, err)
				}

				if compat, ver := upgrader.IsCompatible(sVer); !compat {
					return false, fmt.Errorf("compatibility issue: shar server level %s, client version level: %s: %w", sVer, ver, err)
				}
			}

			// Start a loop keeping this connection alive.
			ctx, cancel := context.WithCancel(ctx)
			defer cancel()
			var fnMx sync.Mutex
			waitCancelSig := make(chan struct{})

			// Acknowledge until waitCancel is closed
			go func(ctx context.Context) {
				select {
				case <-time.After(ackTimeout / 2):

				case <-waitCancelSig:
					cancel()
					return
				}
				fmt.Println("Waiting...")
				fnMx.Lock()
				select {
				case <-waitCancelSig:
					cancel()
					return
				default:
				}
				if err := msg.InProgress(nats.Context(ctx)); err != nil {
					cancel()
					fnMx.Unlock()
					return
				}
				fnMx.Unlock()
			}(ctx)

			ut := &model.WorkflowState{}
			if err := proto.Unmarshal(msg.Data, ut); err != nil {
				log.Error("unmarshaling", err)
				return false, fmt.Errorf("service task listener: %w", err)
			}
			ctx = context.WithValue(ctx, ctxkey.WorkflowInstanceID, ut.WorkflowInstanceId)
			ctx = context.WithValue(ctx, ctxkey.ProcessInstanceID, ut.ProcessInstanceId)
			ctx, err := header.FromMsgHeaderToCtx(ctx, msg.Header)
			if err != nil {
				return true, &errors2.ErrWorkflowFatal{Err: fmt.Errorf("obtain headers from message: %w", err)}
			}
			switch ut.ElementType {
			case element.ServiceTask:
				trackingID := common.TrackingID(ut.Id).ID()
				job, err := c.GetJob(ctx, trackingID)
				if err != nil {
					log.Error("get job", err, slog.String("JobId", trackingID))
					return false, fmt.Errorf("get service task job kv: %w", err)
				}
				svcFn, ok := c.SvcTasks[*job.Execute]
				if !ok {
					log.Error("find service function", err, slog.String("fn", *job.Execute))
					return false, fmt.Errorf("find service task function: %w", err)
				}
				dv, err := vars.Decode(ctx, job.Vars)
				if err != nil {
					log.Error("decode vars", err, slog.String("fn", *job.Execute))
					return false, fmt.Errorf("decode service task job variables: %w", err)
				}
				newVars, err := func() (v model.Vars, e error) {
					defer func() {
						if r := recover(); r != nil {
							v = model.Vars{}
							e = &errors2.ErrWorkflowFatal{Err: fmt.Errorf("call to service task \"%s\" terminated in panic: %w", *ut.Execute, r.(error))}
						}
					}()
					fnMx.Lock()
					pidCtx := context.WithValue(ctx, internalProcessInstanceId, job.ProcessInstanceId)
					v, e = svcFn(pidCtx, &jobClient{cl: c, trackingID: trackingID}, dv)
					close(waitCancelSig)
					fnMx.Unlock()
					return
				}()
				if err != nil {
					var handled bool
					wfe := &workflow.Error{}
					if errors.As(err, wfe) {
						v, err := vars.Encode(ctx, newVars)
						if err != nil {
							return true, &errors2.ErrWorkflowFatal{Err: fmt.Errorf("encode service task variables: %w", err)}
						}
						res := &model.HandleWorkflowErrorResponse{}
						req := &model.HandleWorkflowErrorRequest{TrackingId: trackingID, ErrorCode: wfe.Code, Vars: v}
						if err2 := api2.Call(ctx, c.txCon, messages.APIHandleWorkflowError, c.ExpectedCompatibleServerVersion, req, res); err2 != nil {
							// TODO: This isn't right.  If this call fails it assumes it is handled!
							reterr := fmt.Errorf("handle workflow error: %w", err2)
							return true, logx.Err(ctx, "handle a workflow error", reterr, slog.Any("workflowError", wfe))
						}
						handled = res.Handled
					}
					if !handled {
						log.Warn("execution of service task function", "error", err)
					}
					return wfe.Code != "", err
				}
				err = c.completeServiceTask(ctx, trackingID, newVars)
				ae := &api.Error{}
				if errors.As(err, &ae) {
					if codes.Code(ae.Code) == codes.Internal {
						log.Error("complete service task", err)
						e := &model.Error{
							Id:   "",
							Name: ae.Message,
							Code: "client-" + strconv.Itoa(ae.Code),
						}
						if err := c.cancelWorkflowInstanceWithError(ctx, ut.WorkflowInstanceId, e); err != nil {
							log.Error("cancel workflow instance in response to fatal error", err)
						}
						return true, nil
					}
				} else if errors2.IsWorkflowFatal(err) {
					return true, err
				}
				if err != nil {
					log.Warn("complete service task", err)
					return false, fmt.Errorf("complete service task: %w", err)
				}
				return true, nil

			case element.MessageIntermediateThrowEvent:
				trackingID := common.TrackingID(ut.Id).ID()
				job, err := c.GetJob(ctx, trackingID)
				if err != nil {
					log.Error("get send message task", err, slog.String("JobId", common.TrackingID(ut.Id).ID()))
					return false, fmt.Errorf("complete send message task: %w", err)
				}
				sendFn, ok := c.MsgSender[job.WorkflowName+"_"+*job.Execute]
				if !ok {
					return true, nil
				}

				dv, err := vars.Decode(ctx, job.Vars)
				if err != nil {
					log.Error("decode vars", err, slog.String("fn", *job.Execute))
					return false, &errors2.ErrWorkflowFatal{Err: fmt.Errorf("decode send message variables: %w", err)}
				}
				ctx = context.WithValue(ctx, ctxkey.TrackingID, trackingID)
				pidCtx := context.WithValue(ctx, internalProcessInstanceId, job.ProcessInstanceId)
				if err := sendFn(pidCtx, &messageClient{cl: c, trackingID: trackingID, wfiID: job.WorkflowInstanceId}, dv); err != nil {
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
			return fmt.Errorf("connect to service task consumer: %w", err)
		}
	}
	return nil
}

func (c *Client) listenProcessTerminate(ctx context.Context) error {
	closer := make(chan struct{}, 1)
	err := common.Process(ctx, c.js, "ProcessTerminateConsumer_"+c.ns, closer, subj.NS(messages.WorkflowProcessTerminated, c.ns), "ProcessTerminateConsumer_"+c.ns, 4, func(ctx context.Context, log *slog.Logger, msg *nats.Msg) (bool, error) {
		st := &model.WorkflowState{}
		if err := proto.Unmarshal(msg.Data, st); err != nil {
			log.Error("proto unmarshal error", err)
			return true, fmt.Errorf("listenProcessTerminate unmarshalling proto: %w", err)
		}
		callCtx := context.WithValue(ctx, keys.ContextKey(keys.ProcessInstanceID), st.ProcessInstanceId)
		v, err := vars.Decode(callCtx, st.Vars)
		if err != nil {
			return true, fmt.Errorf("listenProcessTerminate decoding vars: %w", err)
		}
		if fn, ok := c.proCompleteTasks[st.ProcessName]; ok {
			fn(callCtx, v, st.Error, st.State)
		}
		return true, nil
	})
	if err != nil {
		return fmt.Errorf("listen workflow complete process: %w", err)
	}
	return nil
}

// ListUserTaskIDs returns a list of user tasks for a particular owner
func (c *Client) ListUserTaskIDs(ctx context.Context, owner string) (*model.UserTasks, error) {
	res := &model.UserTasks{}
	req := &model.ListUserTasksRequest{Owner: owner}
	if err := api2.Call(ctx, c.txCon, messages.APIListUserTaskIDs, c.ExpectedCompatibleServerVersion, req, res); err != nil {
		return nil, c.clientErr(ctx, err)
	}
	return res, nil
}

// CompleteUserTask completes a task and sends the variables back to the workflow
func (c *Client) CompleteUserTask(ctx context.Context, owner string, trackingID string, newVars model.Vars) error {
	ev, err := vars.Encode(ctx, newVars)
	if err != nil {
		return fmt.Errorf("decode variables for complete user task: %w", err)
	}
	res := &emptypb.Empty{}
	req := &model.CompleteUserTaskRequest{Owner: owner, TrackingId: trackingID, Vars: ev}
	if err := api2.Call(ctx, c.txCon, messages.APICompleteUserTask, c.ExpectedCompatibleServerVersion, req, res); err != nil {
		return c.clientErr(ctx, err)
	}
	return nil
}

func (c *Client) completeServiceTask(ctx context.Context, trackingID string, newVars model.Vars) error {
	ev, err := vars.Encode(ctx, newVars)
	if err != nil {
		return fmt.Errorf("decode variables for complete service task: %w", err)
	}
	res := &emptypb.Empty{}
	req := &model.CompleteServiceTaskRequest{TrackingId: trackingID, Vars: ev}
	if err := api2.Call(ctx, c.txCon, messages.APICompleteServiceTask, c.ExpectedCompatibleServerVersion, req, res); err != nil {
		return c.clientErr(ctx, err)
	}
	return nil
}

func (c *Client) completeSendMessage(ctx context.Context, trackingID string, newVars model.Vars) error {
	ev, err := vars.Encode(ctx, newVars)
	if err != nil {
		return fmt.Errorf("decode variables for complete send message: %w", err)
	}
	res := &emptypb.Empty{}
	req := &model.CompleteSendMessageRequest{TrackingId: trackingID, Vars: ev}
	if err := api2.Call(ctx, c.txCon, messages.APICompleteSendMessageTask, c.ExpectedCompatibleServerVersion, req, res); err != nil {
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
	if err := api2.Call(ctx, c.txCon, messages.APIStoreWorkflow, c.ExpectedCompatibleServerVersion, wf, res); err != nil {
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
	if err := api2.Call(ctx, c.txCon, messages.APIGetWorkflowVersions, c.ExpectedCompatibleServerVersion, req, res); err != nil {
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
	if err := api2.Call(ctx, c.txCon, messages.APIGetWorkflow, c.ExpectedCompatibleServerVersion, req, res); err != nil {
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
	if err := api2.Call(ctx, c.txCon, messages.APICancelWorkflowInstance, c.ExpectedCompatibleServerVersion, req, res); err != nil {
		return c.clientErr(ctx, err)
	}
	return nil
}

// LaunchWorkflow launches a new workflow instance.
func (c *Client) LaunchWorkflow(ctx context.Context, workflowName string, mvars model.Vars) (string, string, error) {
	ev, err := vars.Encode(ctx, mvars)
	if err != nil {
		return "", "", fmt.Errorf("encode variables for launch workflow: %w", err)
	}
	req := &model.LaunchWorkflowRequest{Name: workflowName, Vars: ev}
	res := &model.LaunchWorkflowResponse{}
	if err := api2.Call(ctx, c.txCon, messages.APILaunchWorkflow, c.ExpectedCompatibleServerVersion, req, res); err != nil {
		return "", "", c.clientErr(ctx, err)
	}
	return res.InstanceId, res.WorkflowId, nil
}

// ListWorkflowInstance gets a list of running workflow instances by workflow name.
func (c *Client) ListWorkflowInstance(ctx context.Context, name string) ([]*model.ListWorkflowInstanceResult, error) {
	req := &model.ListWorkflowInstanceRequest{WorkflowName: name}
	res := &model.ListWorkflowInstanceResponse{}
	if err := api2.Call(ctx, c.txCon, messages.APIListWorkflowInstance, c.ExpectedCompatibleServerVersion, req, res); err != nil {
		return nil, c.clientErr(ctx, err)
	}
	return res.Result, nil
}

// ListWorkflows gets a list of launchable workflow in SHAR.
func (c *Client) ListWorkflows(ctx context.Context) ([]*model.ListWorkflowResult, error) {
	req := &emptypb.Empty{}
	res := &model.ListWorkflowsResponse{}
	if err := api2.Call(ctx, c.txCon, messages.APIListWorkflows, c.ExpectedCompatibleServerVersion, req, res); err != nil {
		return nil, c.clientErr(ctx, err)
	}
	return res.Result, nil
}

// ListWorkflowInstanceProcesses lists the current process IDs for a workflow instance.
func (c *Client) ListWorkflowInstanceProcesses(ctx context.Context, id string) (*model.ListWorkflowInstanceProcessesResult, error) {
	req := &model.ListWorkflowInstanceProcessesRequest{Id: id}
	res := &model.ListWorkflowInstanceProcessesResult{}
	if err := api2.Call(ctx, c.txCon, messages.APIListWorkflowInstanceProcesses, c.ExpectedCompatibleServerVersion, req, res); err != nil {
		return nil, c.clientErr(ctx, err)
	}
	return res, nil
}

// GetProcessInstanceStatus lists the current workflow states for a process instance.
func (c *Client) GetProcessInstanceStatus(ctx context.Context, id string) (*model.GetProcessInstanceStatusResult, error) {
	req := &model.GetProcessInstanceStatusRequest{Id: id}
	res := &model.GetProcessInstanceStatusResult{}
	if err := api2.Call(ctx, c.txCon, messages.APIGetProcessInstanceStatus, c.ExpectedCompatibleServerVersion, req, res); err != nil {
		return nil, c.clientErr(ctx, err)
	}
	return res, nil
}

func (c *Client) getServiceTaskRoutingID(ctx context.Context, name string) (string, error) {
	req := &model.GetServiceTaskRoutingIDRequest{Name: name}
	res := &model.GetServiceTaskRoutingIDResponse{}
	if err := api2.Call(ctx, c.txCon, messages.APIGetServiceTaskRoutingID, c.ExpectedCompatibleServerVersion, req, res); err != nil {
		return "", c.clientErr(ctx, err)
	}
	return res.Id, nil
}

// GetUserTask fetches details for a user task based upon an ID obtained from, ListUserTasks
func (c *Client) GetUserTask(ctx context.Context, owner string, trackingID string) (*model.GetUserTaskResponse, model.Vars, error) {
	req := &model.GetUserTaskRequest{Owner: owner, TrackingId: trackingID}
	res := &model.GetUserTaskResponse{}
	if err := api2.Call(ctx, c.txCon, messages.APIGetUserTask, c.ExpectedCompatibleServerVersion, req, res); err != nil {
		return nil, nil, c.clientErr(ctx, err)
	}
	v, err := vars.Decode(ctx, res.Vars)
	if err != nil {
		return nil, nil, c.clientErr(ctx, err)
	}
	return res, v, nil
}

// SendMessage sends a Workflow Message to a specific workflow instance
func (c *Client) SendMessage(ctx context.Context, name string, key any, mvars model.Vars) error {
	var skey string
	switch key.(type) {
	case string:
		skey = "\"" + fmt.Sprintf("%+v", key) + "\""
	default:
		skey = fmt.Sprintf("%+v", key)
	}
	b, err := vars.Encode(ctx, mvars)
	if err != nil {
		return fmt.Errorf("encode variables for send message: %w", err)
	}
	req := &model.SendMessageRequest{Name: name, CorrelationKey: skey, Vars: b}
	res := &emptypb.Empty{}
	if err := api2.Call(ctx, c.txCon, messages.APISendMessage, c.ExpectedCompatibleServerVersion, req, res); err != nil {
		return c.clientErr(ctx, err)
	}
	return nil
}

func (c *Client) clientErr(_ context.Context, err error) error {
	return fmt.Errorf("client error: %w", err)
}

// RegisterProcessComplete registers a function to be executed when a shar workflow process terminates.
func (c *Client) RegisterProcessComplete(processId string, fn ProcessTerminateFn) error {
	c.proCompleteTasks[processId] = fn
	return nil
}

// GetServerInstanceStats is an unsupported function to obtain server metrics
func (c *Client) GetServerInstanceStats(ctx context.Context) (*model.WorkflowStats, error) {
	req := &emptypb.Empty{}
	res := &model.WorkflowStats{}
	if err := api2.Call(ctx, c.txCon, messages.APIGetServerInstanceStats, c.ExpectedCompatibleServerVersion, req, res); err != nil {
		return nil, c.clientErr(ctx, err)
	}
	return res, nil
}

// GetProcessHistory gets the history for a process.
func (c *Client) GetProcessHistory(ctx context.Context, processInstanceId string) (*model.GetProcessHistoryResponse, error) {
	req := &model.GetProcessHistoryRequest{Id: processInstanceId}
	res := &model.GetProcessHistoryResponse{}
	if err := api2.Call(ctx, c.txCon, messages.APIGetProcessHistory, c.ExpectedCompatibleServerVersion, req, res); err != nil {
		return nil, c.clientErr(ctx, err)
	}
	return res, nil
}

func (c *Client) clientLog(ctx context.Context, trackingID string, severity messages.WorkflowLogLevel, code int32, message string, attrs map[string]string) error {
	if err := common.Log(ctx, c.txJS, trackingID, model.LogSource_logSourceJob, severity, code, message, attrs); err != nil {
		return fmt.Errorf("client log: %w", err)
	}
	return nil
}

// GetJob returns a Job given a tracking ID
func (c *Client) GetJob(ctx context.Context, id string) (*model.WorkflowState, error) {
	job := &model.WorkflowState{}
	// TODO: Stop direct data read
	if err := common.LoadObj(ctx, c.job, id, job); err != nil {
		return nil, fmt.Errorf("load object for get job: %w", err)
	}
	return job, nil
}

// GetServerVersion returns the current server version
func (c *Client) GetServerVersion(ctx context.Context) (*version.Version, error) {
	req := &model.GetVersionInfoRequest{
		ClientVersion: c.version.String(),
	}
	res := &model.GetVersionInfoResponse{}
	if err := api2.Call(ctx, c.con, messages.APIGetVersionInfo, c.ExpectedCompatibleServerVersion, req, res); err != nil {
		return nil, fmt.Errorf("get version info: %w", err)
	}

	sv, err := version.NewVersion(res.ServerVersion)
	if err != nil {
		return nil, fmt.Errorf("get server version info: %w", err)
	}
	cv, err := version.NewVersion(res.MinCompatibleVersion)
	if err != nil {
		return nil, fmt.Errorf("get server version info: %w", err)
	}
	c.ExpectedServerVersion = sv
	c.ExpectedCompatibleServerVersion = cv

	if !res.Connect {
		return sv, fmt.Errorf("incompatible client version: client must be " + cv.String())
	}

	ok, cv2 := upgrader.IsCompatible(sv)
	if !ok {
		return sv, fmt.Errorf("incompatible server version: " + sv.String() + " server must be " + cv2.String())
	}
	return sv, nil
}
