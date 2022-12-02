package api

import (
	"context"
	"errors"
	"fmt"
	"gitlab.com/shar-workflow/shar/common/logx"
	"gitlab.com/shar-workflow/shar/common/subj"
	"gitlab.com/shar-workflow/shar/server/vars"
	"golang.org/x/exp/slog"
	"sync"

	"github.com/nats-io/nats.go"
	"gitlab.com/shar-workflow/shar/common"
	"gitlab.com/shar-workflow/shar/model"
	errors2 "gitlab.com/shar-workflow/shar/server/errors"
	"gitlab.com/shar-workflow/shar/server/messages"
	"gitlab.com/shar-workflow/shar/server/services"
	"gitlab.com/shar-workflow/shar/server/workflow"
	"google.golang.org/grpc/codes"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/emptypb"
	"google.golang.org/protobuf/types/known/wrapperspb"
)

// SharServer provides API endpoints for SHAR
type SharServer struct {
	ns            *services.NatsService
	engine        *workflow.Engine
	subs          *sync.Map
	panicRecovery bool
}

// New creates a new instance of the SHAR API server
func New(ns *services.NatsService, panicRecovery bool) (*SharServer, error) {
	engine, err := workflow.NewEngine(ns)
	if err != nil {
		return nil, fmt.Errorf("failed to create SHAR engine instance: %w", err)
	}
	if err := engine.Start(context.Background()); err != nil {
		return nil, fmt.Errorf("failed to start SHAR engine: %w", err)
	}
	return &SharServer{
		ns:            ns,
		engine:        engine,
		panicRecovery: panicRecovery,
		subs:          &sync.Map{},
	}, nil
}

func (s *SharServer) storeWorkflow(ctx context.Context, wf *model.Workflow) (*wrapperspb.StringValue, error) {
	res, err := s.engine.LoadWorkflow(ctx, wf)
	if err != nil {
		return nil, fmt.Errorf("failed to store workflow: %w", err)
	}
	return &wrapperspb.StringValue{Value: res}, nil
}

func (s *SharServer) getServiceTaskRoutingID(ctx context.Context, taskName *wrapperspb.StringValue) (*wrapperspb.StringValue, error) {
	res, err := s.ns.GetServiceTaskRoutingKey(ctx, taskName.Value)
	if err != nil {
		return nil, fmt.Errorf("failed to get service task routing id: %w", err)
	}
	return &wrapperspb.StringValue{Value: res}, nil
}

func (s *SharServer) getMessageSenderRoutingID(ctx context.Context, req *model.GetMessageSenderRoutingIdRequest) (*wrapperspb.StringValue, error) {
	res, err := s.ns.GetMessageSenderRoutingKey(ctx, req.WorkflowName, req.MessageName)
	if err != nil {
		return nil, fmt.Errorf("failed to get message sender routing id: %w", err)
	}
	return &wrapperspb.StringValue{Value: res}, nil
}

func (s *SharServer) launchWorkflow(ctx context.Context, req *model.LaunchWorkflowRequest) (*wrapperspb.StringValue, error) {
	res, err := s.engine.Launch(ctx, req.Name, req.Vars)
	if err != nil {
		return nil, fmt.Errorf("failed to launch workflow instance kv: %w", err)
	}
	return &wrapperspb.StringValue{Value: res}, nil
}

func (s *SharServer) cancelWorkflowInstance(ctx context.Context, req *model.CancelWorkflowInstanceRequest) (*emptypb.Empty, error) {
	err := s.engine.CancelWorkflowInstance(ctx, req.Id, req.State, req.Error)
	if err != nil {
		return nil, fmt.Errorf("failed to cancel workflow instance kv: %w", err)
	}
	return &emptypb.Empty{}, nil
}

func (s *SharServer) listWorkflowInstance(ctx context.Context, req *model.ListWorkflowInstanceRequest) (*model.ListWorkflowInstanceResponse, error) {
	wch, errs := s.ns.ListWorkflowInstance(ctx, req.WorkflowName)
	ret := make([]*model.ListWorkflowInstanceResult, 0)
	for {
		select {
		case winf := <-wch:
			if winf == nil {
				return &model.ListWorkflowInstanceResponse{Result: ret}, nil
			}
			ret = append(ret, &model.ListWorkflowInstanceResult{
				Id:      winf.Id,
				Version: winf.Version,
			})
		case err := <-errs:
			return nil, fmt.Errorf("failed to list workflow instancesr: %w", err)
		}
	}
}

func (s *SharServer) getWorkflowInstanceStatus(ctx context.Context, req *model.GetWorkflowInstanceStatusRequest) (*model.WorkflowInstanceStatus, error) {
	res, err := s.ns.GetWorkflowInstanceStatus(ctx, req.Id)
	if err != nil {
		return nil, fmt.Errorf("failed to get workflow instance status: %w", err)
	}
	return res, nil
}

func (s *SharServer) listWorkflows(ctx context.Context, _ *emptypb.Empty) (*model.ListWorkflowsResponse, error) {
	res, errs := s.ns.ListWorkflows(ctx)
	ret := make([]*model.ListWorkflowResult, 0)
	for {
		select {
		case winf := <-res:
			if winf == nil {
				return &model.ListWorkflowsResponse{Result: ret}, nil
			}
			ret = append(ret, &model.ListWorkflowResult{
				Name:    winf.Name,
				Version: winf.Version,
			})
		case err := <-errs:
			return nil, fmt.Errorf("failed to list workflowsr: %w", err)
		}
	}
}

func (s *SharServer) sendMessage(ctx context.Context, req *model.SendMessageRequest) (*emptypb.Empty, error) {
	if err := s.ns.PublishMessage(ctx, req.WorkflowInstanceId, req.Name, req.Key, req.Vars); err != nil {
		return nil, fmt.Errorf("failed to send message: %w", err)
	}
	return &emptypb.Empty{}, nil
}

func (s *SharServer) completeManualTask(ctx context.Context, req *model.CompleteManualTaskRequest) (*emptypb.Empty, error) {
	if err := s.engine.CompleteManualTask(ctx, req.TrackingId, req.Vars); err != nil {
		return nil, fmt.Errorf("failed to complete manual task: %w", err)
	}
	return &emptypb.Empty{}, nil
}

func (s *SharServer) completeServiceTask(ctx context.Context, req *model.CompleteServiceTaskRequest) (*emptypb.Empty, error) {
	if err := s.engine.CompleteServiceTask(ctx, req.TrackingId, req.Vars); err != nil {
		return nil, fmt.Errorf("failed to complete service task: %w", err)
	}
	return &emptypb.Empty{}, nil
}

func (s *SharServer) completeSendMessageTask(ctx context.Context, req *model.CompleteSendMessageRequest) (*emptypb.Empty, error) {
	if err := s.engine.CompleteSendMessageTask(ctx, req.TrackingId, req.Vars); err != nil {
		return nil, fmt.Errorf("failed to complete send message task: %w", err)
	}
	return &emptypb.Empty{}, nil
}

func (s *SharServer) completeUserTask(ctx context.Context, req *model.CompleteUserTaskRequest) (*emptypb.Empty, error) {
	if err := s.engine.CompleteUserTask(ctx, req.TrackingId, req.Vars); err != nil {
		return nil, fmt.Errorf("failed to complete user task: %w", err)
	}
	return &emptypb.Empty{}, nil
}

var shutdownOnce sync.Once

// Shutdown gracefully shuts down the SHAR API server and Engine
func (s *SharServer) Shutdown() {
	slog.Info("stopping shar api listener")
	shutdownOnce.Do(func() {
		s.subs.Range(func(key, _ any) bool {
			sub := key.(*nats.Subscription)
			if err := sub.Drain(); err != nil {
				slog.Error("Could not drain subscription for "+sub.Subject, err)
				return false
			}
			return true
		})
		s.engine.Shutdown()
		slog.Info("shar api listener stopped")
	})
}

// Listen starts the SHAR API server listening to incoming requests
func (s *SharServer) Listen() error {
	con := s.ns.Conn()
	if _, err := listen(con, s.panicRecovery, s.subs, messages.APIStoreWorkflow, &model.Workflow{}, s.storeWorkflow); err != nil {
		return fmt.Errorf("APIStoreWorkflow failed: %w", err)
	}
	if _, err := listen(con, s.panicRecovery, s.subs, messages.APICancelWorkflowInstance, &model.CancelWorkflowInstanceRequest{}, s.cancelWorkflowInstance); err != nil {
		return fmt.Errorf("APICancelWorkflowInstance failed: %w", err)
	}
	if _, err := listen(con, s.panicRecovery, s.subs, messages.APILaunchWorkflow, &model.LaunchWorkflowRequest{}, s.launchWorkflow); err != nil {
		return fmt.Errorf("APILaunchWorkflow failed: %w", err)
	}
	if _, err := listen(con, s.panicRecovery, s.subs, messages.APIListWorkflows, &emptypb.Empty{}, s.listWorkflows); err != nil {
		return fmt.Errorf("APIListWorkflows failed: %w", err)
	}
	if _, err := listen(con, s.panicRecovery, s.subs, messages.APIGetWorkflowStatus, &model.GetWorkflowInstanceStatusRequest{}, s.getWorkflowInstanceStatus); err != nil {
		return fmt.Errorf("APIGetWorkflowStatus failed: %w", err)
	}
	if _, err := listen(con, s.panicRecovery, s.subs, messages.APIListWorkflowInstance, &model.ListWorkflowInstanceRequest{}, s.listWorkflowInstance); err != nil {
		return fmt.Errorf("APIListWorkflowInstance failed: %w", err)
	}
	if _, err := listen(con, s.panicRecovery, s.subs, messages.APISendMessage, &model.SendMessageRequest{}, s.sendMessage); err != nil {
		return fmt.Errorf("APISendMessage failed: %w", err)
	}
	if _, err := listen(con, s.panicRecovery, s.subs, messages.APICompleteManualTask, &model.CompleteManualTaskRequest{}, s.completeManualTask); err != nil {
		return fmt.Errorf("APICompleteManualTask failed: %w", err)
	}
	if _, err := listen(con, s.panicRecovery, s.subs, messages.APICompleteServiceTask, &model.CompleteServiceTaskRequest{}, s.completeServiceTask); err != nil {
		return fmt.Errorf("APICompleteServiceTask failed: %w", err)
	}
	if _, err := listen(con, s.panicRecovery, s.subs, messages.APICompleteUserTask, &model.CompleteUserTaskRequest{}, s.completeUserTask); err != nil {
		return fmt.Errorf("APICompleteUserTask failed: %w", err)
	}
	if _, err := listen(con, s.panicRecovery, s.subs, messages.APIListUserTaskIDs, &model.ListUserTasksRequest{}, s.listUserTaskIDs); err != nil {
		return fmt.Errorf("APIListUserTaskIDs failed: %w", err)
	}
	if _, err := listen(con, s.panicRecovery, s.subs, messages.APIGetUserTask, &model.GetUserTaskRequest{}, s.getUserTask); err != nil {
		return fmt.Errorf("APIGetUserTask failed: %w", err)
	}
	if _, err := listen(con, s.panicRecovery, s.subs, messages.APIHandleWorkflowError, &model.HandleWorkflowErrorRequest{}, s.handleWorkflowError); err != nil {
		return fmt.Errorf("APIHandleWorkflowError failed: %w", err)
	}
	if _, err := listen(con, s.panicRecovery, s.subs, messages.APIGetServerInstanceStats, &emptypb.Empty{}, s.getServerInstanceStats); err != nil {
		return fmt.Errorf("APIGetServerInstanceStats failed: %w", err)
	}

	if _, err := listen(con, s.panicRecovery, s.subs, messages.APIGetServiceTaskRoutingID, &wrapperspb.StringValue{}, s.getServiceTaskRoutingID); err != nil {
		return fmt.Errorf("APIGetServiceTaskRoutingID failed: %w", err)
	}

	if _, err := listen(con, s.panicRecovery, s.subs, messages.APIGetMessageSenderRoutingID, &model.GetMessageSenderRoutingIdRequest{}, s.getMessageSenderRoutingID); err != nil {
		return fmt.Errorf("APIGetMessageSenderRoutingID failed: %w", err)
	}

	if _, err := listen(con, s.panicRecovery, s.subs, messages.APICompleteSendMessageTask, &model.CompleteSendMessageRequest{}, s.completeSendMessageTask); err != nil {
		return fmt.Errorf("APICompleteSendMessageTask failed: %w", err)
	}

	slog.Info("shar api listener started")
	return nil
}

func (s *SharServer) listUserTaskIDs(ctx context.Context, req *model.ListUserTasksRequest) (*model.UserTasks, error) {
	oid, err := s.ns.OwnerID(req.Owner)
	if err != nil {
		return nil, fmt.Errorf("failed to get owner ID: %w", err)
	}
	ut, err := s.ns.GetUserTaskIDs(ctx, oid)
	if errors.Is(err, nats.ErrKeyNotFound) {
		return &model.UserTasks{Id: []string{}}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get user task IDs: %w", err)
	}
	return ut, nil
}

func (s *SharServer) getUserTask(ctx context.Context, req *model.GetUserTaskRequest) (*model.GetUserTaskResponse, error) {
	job, err := s.ns.GetJob(ctx, req.TrackingId)
	if err != nil {
		return nil, fmt.Errorf("get user task failed to get job: %w", err)
	}
	wf, err := s.ns.GetWorkflow(ctx, job.WorkflowId)
	if err != nil {
		return nil, fmt.Errorf("get user task failed to get workflow: %w", err)
	}
	els := make(map[string]*model.Element)
	for _, v := range wf.Process {
		common.IndexProcessElements(v.Elements, els)
	}
	return &model.GetUserTaskResponse{
		TrackingId:  common.TrackingID(job.Id).ID(),
		Owner:       req.Owner,
		Name:        els[job.ElementId].Name,
		Description: els[job.ElementId].Documentation,
		Vars:        job.Vars,
	}, nil
}

func (s *SharServer) handleWorkflowError(ctx context.Context, req *model.HandleWorkflowErrorRequest) (*model.HandleWorkflowErrorResponse, error) {
	// Sanity check
	if req.ErrorCode == "" {
		return nil, fmt.Errorf("ErrorCode may not be empty: %w", errors2.ErrMissingErrorCode)
	}

	// First get the job that the error occurred in
	job, err := s.ns.GetJob(ctx, req.TrackingId)
	if err != nil {
		return nil, fmt.Errorf("failed to get job for handle workflow error: %w", err)
	}

	// Get the workflow, so we can look up the error definitions
	wf, err := s.ns.GetWorkflow(ctx, job.WorkflowId)
	if err != nil {
		return nil, fmt.Errorf("failed to get workflow definition for handle workflow error: %w", err)
	}

	// Get the element corresponding to the job
	els := common.ElementTable(wf)

	// Get the current element
	el := els[job.ElementId]

	// Get the errors supported by this workflow
	var found bool
	wfErrs := make(map[string]*model.Error)
	for _, v := range wf.Errors {
		if v.Code == req.ErrorCode {
			found = true
		}
		wfErrs[v.Id] = v
	}
	if !found {
		werr := &errors2.ErrWorkflowFatal{Err: fmt.Errorf("workflow-fatal: can't handle error code %s as the workflow doesn't support it: %w", req.ErrorCode, errors2.ErrWorkflowErrorNotFound)}
		if _, err := s.cancelWorkflowInstance(ctx, &model.CancelWorkflowInstanceRequest{Id: job.WorkflowInstanceId, State: model.CancellationState_errored}); err != nil {
			return nil, fmt.Errorf("failed to cancel workflow instance: %w", werr)
		}
		// TODO: This always assumes service task.  Wrong!
		if err := s.ns.PublishWorkflowState(ctx, subj.NS(messages.WorkflowJobServiceTaskAbort, "default"), job); err != nil {
			return nil, fmt.Errorf("failed to cencel job: %w", werr)
		}

		return nil, fmt.Errorf("workflow halted: %w", werr)
	}

	// Get the errors associated with this element
	var errDef *model.Error
	var caughtError *model.CatchError
	for _, v := range el.Errors {
		wfErr := wfErrs[v.ErrorId]
		if req.ErrorCode == wfErr.Code {
			errDef = wfErr
			caughtError = v
			break
		}
	}

	if errDef == nil {
		return &model.HandleWorkflowErrorResponse{Handled: false}, nil
	}

	// Get the target workflow activity
	target := els[caughtError.Target]

	oldState, err := s.ns.GetOldState(ctx, common.TrackingID(job.Id).Pop().ID())
	if err != nil {
		return nil, fmt.Errorf("failed to get old state for handle workflow error: %w", err)
	}
	if err := vars.OutputVars(ctx, req.Vars, &oldState.Vars, caughtError.OutputTransform); err != nil {
		return nil, &errors2.ErrWorkflowFatal{Err: err}
	}
	if err := s.ns.PublishWorkflowState(ctx, messages.WorkflowTraversalExecute, &model.WorkflowState{
		ElementType:        target.Type,
		ElementId:          target.Id,
		WorkflowId:         job.WorkflowId,
		WorkflowInstanceId: job.WorkflowInstanceId,
		Id:                 common.TrackingID(job.Id).Pop().Pop(),
		Vars:               oldState.Vars,
	}); err != nil {
		log := slog.FromContext(ctx)
		log.Error("failed to publish workflow state", err)
		return nil, fmt.Errorf("failed to publish traversal for handle workflow error: %w", err)
	}
	// TODO: This always assumes service task.  Wrong!
	if err := s.ns.PublishWorkflowState(ctx, messages.WorkflowJobServiceTaskAbort, &model.WorkflowState{
		ElementType:        target.Type,
		ElementId:          target.Id,
		WorkflowId:         job.WorkflowId,
		WorkflowInstanceId: job.WorkflowInstanceId,
		Id:                 job.Id,
		Vars:               job.Vars,
	}); err != nil {
		log := slog.FromContext(ctx)
		log.Error("failed to publish workflow state", err)
		// We have already traversed so retunring an error here would be incorrect.
		// It would force reprocessing and possibly double traversing
		// TODO: develop an idempotent behaviour based upon hash nats message ids + deduplication
		return nil, fmt.Errorf("failed to publish abort task for handle workflow error: %w", err)
	}
	return &model.HandleWorkflowErrorResponse{Handled: true}, nil
}

func (s *SharServer) getServerInstanceStats(_ context.Context, _ *emptypb.Empty) (*model.WorkflowStats, error) {
	ret := *s.ns.WorkflowStats()
	return &ret, nil
}

func listen[T proto.Message, U proto.Message](con common.NatsConn, panicRecovery bool, subList *sync.Map, subject string, req T, fn func(ctx context.Context, req T) (U, error)) (*nats.Subscription, error) {
	sub, err := con.QueueSubscribe(subject, subject, func(msg *nats.Msg) {
		ctx, log := logx.LoggingEntrypoint(context.Background(), "server", msg.Header.Get(logx.CorrelationHeader))
		if err := callAPI(ctx, panicRecovery, req, msg, fn); err != nil {
			log.Error("API call for "+subject+" failed", err)
		}
	})
	if err != nil {
		return nil, fmt.Errorf("failed to subscribe to %s: %w", subject, err)
	}
	subList.Store(sub, struct{}{})
	return sub, nil
}

func callAPI[T proto.Message, U proto.Message](ctx context.Context, panicRecovery bool, container T, msg *nats.Msg, fn func(ctx context.Context, req T) (U, error)) error {
	if panicRecovery {
		defer recoverAPIpanic(msg)
	}
	if err := proto.Unmarshal(msg.Data, container); err != nil {
		errorResponse(msg, codes.InvalidArgument, err.Error())
		return fmt.Errorf("failed to unmarshal message data during callAPI: %w", err)
	}
	cid := msg.Header.Get(logx.CorrelationHeader)
	if cid == "" {
		errorResponse(msg, codes.InvalidArgument, errors2.ErrMissingCorrelation)
		return fmt.Errorf("failed to get correlation key : %w", errors2.ErrMissingCorrelation)
	}
	ctx = context.WithValue(ctx, logx.CorrelationContextKey, cid)
	resMsg, err := fn(ctx, container)
	if err != nil {
		c := codes.Unknown
		if errors2.IsWorkflowFatal(err) {
			c = codes.Internal
		}
		errorResponse(msg, c, err.Error())
		return fmt.Errorf("failed during API call: %w", err)
	}
	res, err := proto.Marshal(resMsg)
	if err != nil {
		errorResponse(msg, codes.InvalidArgument, err.Error())
		return fmt.Errorf("failed to unmarshal API response: %w", err)
	}
	if err := msg.Respond(res); err != nil {
		errorResponse(msg, codes.FailedPrecondition, err.Error())
		return fmt.Errorf("failed during API response: %w", err)
	}
	return nil
}

func recoverAPIpanic(msg *nats.Msg) {
	if r := recover(); r != nil {
		errorResponse(msg, codes.Internal, r)
		slog.Info("recovered from ", r)
	}
}

func errorResponse(m *nats.Msg, code codes.Code, msg any) {
	if err := m.Respond(apiError(code, msg)); err != nil {
		slog.Error("failed to send error response: "+string(apiError(codes.Internal, msg)), err)
	}
}

func apiError(code codes.Code, msg any) []byte {
	return []byte(fmt.Sprintf("ERR_%d|%+v", code, msg))
}
