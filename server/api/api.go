package api

import (
	"context"
	"errors"
	"fmt"
	"github.com/nats-io/nats.go"
	"gitlab.com/shar-workflow/shar/common"
	"gitlab.com/shar-workflow/shar/model"
	errors2 "gitlab.com/shar-workflow/shar/server/errors"
	"gitlab.com/shar-workflow/shar/server/messages"
	"gitlab.com/shar-workflow/shar/server/services"
	"gitlab.com/shar-workflow/shar/server/workflow"
	"go.uber.org/zap"
	"google.golang.org/grpc/codes"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/emptypb"
	"google.golang.org/protobuf/types/known/wrapperspb"
	"sync"
)

type SharServer struct {
	log           *zap.Logger
	ns            *services.NatsService
	engine        *workflow.Engine
	subs          map[*nats.Subscription]struct{}
	panicRecovery bool
}

func New(log *zap.Logger, ns *services.NatsService, panicRecovery bool) (*SharServer, error) {
	engine, err := workflow.NewEngine(log, ns)
	if err != nil {
		return nil, err
	}
	if err := engine.Start(context.Background()); err != nil {
		panic(err)
	}
	return &SharServer{
		log:           log,
		ns:            ns,
		engine:        engine,
		panicRecovery: panicRecovery,
		subs:          make(map[*nats.Subscription]struct{}),
	}, nil
}

func (s *SharServer) storeWorkflow(ctx context.Context, wf *model.Workflow) (*wrapperspb.StringValue, error) {
	res, err := s.engine.LoadWorkflow(ctx, wf)
	return &wrapperspb.StringValue{Value: res}, err
}

func (s *SharServer) getServiceTaskRoutingID(ctx context.Context, taskName *wrapperspb.StringValue) (*wrapperspb.StringValue, error) {
	res, err := s.ns.GetServiceTaskRoutingKey(taskName.Value)
	return &wrapperspb.StringValue{Value: res}, err
}

func (s *SharServer) getMessageSenderRoutingID(ctx context.Context, req *model.GetMessageSenderRoutingIdRequest) (*wrapperspb.StringValue, error) {
	res, err := s.ns.GetMessageSenderRoutingKey(req.WorkflowName, req.MessageName)
	return &wrapperspb.StringValue{Value: res}, err
}

func (s *SharServer) launchWorkflow(ctx context.Context, req *model.LaunchWorkflowRequest) (*wrapperspb.StringValue, error) {
	res, err := s.engine.Launch(ctx, req.Name, req.Vars)
	return &wrapperspb.StringValue{Value: res}, err
}

func (s *SharServer) cancelWorkflowInstance(ctx context.Context, req *model.CancelWorkflowInstanceRequest) (*emptypb.Empty, error) {
	err := s.engine.CancelWorkflowInstance(ctx, req.Id, req.State, req.Error)
	if err != nil {
		return nil, err
	}
	return &emptypb.Empty{}, err
}

func (s *SharServer) listWorkflowInstance(ctx context.Context, req *model.ListWorkflowInstanceRequest) (*model.ListWorkflowInstanceResponse, error) {
	wch, errs := s.ns.ListWorkflowInstance(ctx, req.WorkflowName)
	ret := make([]*model.ListWorkflowInstanceResult, 0)
	var done bool
	for !done {
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
			return nil, err
		}
	}
	return &model.ListWorkflowInstanceResponse{Result: ret}, nil
}
func (s *SharServer) getWorkflowInstanceStatus(ctx context.Context, req *model.GetWorkflowInstanceStatusRequest) (*model.WorkflowInstanceStatus, error) {
	res, err := s.ns.GetWorkflowInstanceStatus(ctx, req.Id)
	if err != nil {
		return nil, err
	}
	return res, nil
}

func (s *SharServer) listWorkflows(ctx context.Context, _ *emptypb.Empty) (*model.ListWorkflowsResponse, error) {
	res, errs := s.ns.ListWorkflows(ctx)
	ret := make([]*model.ListWorkflowResult, 0)
	var done bool
	for !done {
		select {
		case winf := <-res:
			if winf == nil {
				done = true
				break
			}
			ret = append(ret, &model.ListWorkflowResult{
				Name:    winf.Name,
				Version: winf.Version,
			})
		case err := <-errs:
			return nil, err
		}
	}
	return &model.ListWorkflowsResponse{Result: ret}, nil
}

func (s *SharServer) sendMessage(ctx context.Context, req *model.SendMessageRequest) (*emptypb.Empty, error) {
	if err := s.ns.PublishMessage(ctx, req.WorkflowInstanceId, req.Name, req.Key, req.Vars); err != nil {
		return nil, err
	}
	return &emptypb.Empty{}, nil
}

func (s *SharServer) completeManualTask(ctx context.Context, req *model.CompleteManualTaskRequest) (*emptypb.Empty, error) {
	return &emptypb.Empty{}, s.engine.CompleteManualTask(ctx, req.TrackingId, req.Vars)
}

func (s *SharServer) completeServiceTask(ctx context.Context, req *model.CompleteServiceTaskRequest) (*emptypb.Empty, error) {
	return &emptypb.Empty{}, s.engine.CompleteServiceTask(ctx, req.TrackingId, req.Vars)
}

func (s *SharServer) completeUserTask(ctx context.Context, req *model.CompleteUserTaskRequest) (*emptypb.Empty, error) {
	return &emptypb.Empty{}, s.engine.CompleteUserTask(ctx, req.TrackingId, req.Vars)
}

var shutdownOnce sync.Once

func (s *SharServer) Shutdown() {
	s.log.Info("stopping shar api listener")
	shutdownOnce.Do(func() {
		for sub := range s.subs {
			err := sub.Drain()
			if err != nil {
				s.log.Error("Could not drain subscription for "+sub.Subject, zap.Error(err))
			}
		}
		s.engine.Shutdown()
		s.log.Info("shar api listener stopped")
	})
}

func (s *SharServer) Listen() error {
	con := s.ns.Conn()
	log := s.log
	if _, err := listen(con, log, s.panicRecovery, s.subs, messages.ApiStoreWorkflow, &model.Workflow{}, s.storeWorkflow); err != nil {
		return err
	}
	if _, err := listen(con, log, s.panicRecovery, s.subs, messages.ApiCancelWorkflowInstance, &model.CancelWorkflowInstanceRequest{}, s.cancelWorkflowInstance); err != nil {
		return err
	}
	if _, err := listen(con, log, s.panicRecovery, s.subs, messages.ApiLaunchWorkflow, &model.LaunchWorkflowRequest{}, s.launchWorkflow); err != nil {
		return err
	}
	if _, err := listen(con, log, s.panicRecovery, s.subs, messages.ApiListWorkflows, &emptypb.Empty{}, s.listWorkflows); err != nil {
		return err
	}
	if _, err := listen(con, log, s.panicRecovery, s.subs, messages.ApiGetWorkflowStatus, &model.GetWorkflowInstanceStatusRequest{}, s.getWorkflowInstanceStatus); err != nil {
		return err
	}
	if _, err := listen(con, log, s.panicRecovery, s.subs, messages.ApiListWorkflowInstance, &model.ListWorkflowInstanceRequest{}, s.listWorkflowInstance); err != nil {
		return err
	}
	if _, err := listen(con, log, s.panicRecovery, s.subs, messages.ApiSendMessage, &model.SendMessageRequest{}, s.sendMessage); err != nil {
		return err
	}
	if _, err := listen(con, log, s.panicRecovery, s.subs, messages.ApiCompleteManualTask, &model.CompleteManualTaskRequest{}, s.completeManualTask); err != nil {
		return err
	}
	if _, err := listen(con, log, s.panicRecovery, s.subs, messages.ApiCompleteServiceTask, &model.CompleteServiceTaskRequest{}, s.completeServiceTask); err != nil {
		return err
	}
	if _, err := listen(con, log, s.panicRecovery, s.subs, messages.ApiCompleteUserTask, &model.CompleteUserTaskRequest{}, s.completeUserTask); err != nil {
		return err
	}
	if _, err := listen(con, log, s.panicRecovery, s.subs, messages.ApiListUserTaskIDs, &model.ListUserTasksRequest{}, s.listUserTaskIDs); err != nil {
		return err
	}
	if _, err := listen(con, log, s.panicRecovery, s.subs, messages.ApiGetUserTask, &model.GetUserTaskRequest{}, s.getUserTask); err != nil {
		return err
	}
	if _, err := listen(con, log, s.panicRecovery, s.subs, messages.ApiHandleWorkflowError, &model.HandleWorkflowErrorRequest{}, s.handleWorkflowError); err != nil {
		return err
	}
	if _, err := listen(con, log, s.panicRecovery, s.subs, messages.ApiGetServerInstanceStats, &emptypb.Empty{}, s.getServerInstanceStats); err != nil {
		return err
	}

	if _, err := listen(con, log, s.panicRecovery, s.subs, messages.ApiGetServiceTaskRoutingID, &wrapperspb.StringValue{}, s.getServiceTaskRoutingID); err != nil {
		return err
	}

	if _, err := listen(con, log, s.panicRecovery, s.subs, messages.ApiGetMessageSenderRoutingID, &model.GetMessageSenderRoutingIdRequest{}, s.getMessageSenderRoutingID); err != nil {
		return err
	}

	s.log.Info("shar api listener started")
	return nil
}

func (s *SharServer) listUserTaskIDs(ctx context.Context, req *model.ListUserTasksRequest) (*model.UserTasks, error) {
	oid, err := s.ns.OwnerId(req.Owner)
	if err != nil {
		return nil, err
	}
	ut, err := s.ns.GetUserTaskIDs(ctx, oid)
	if errors.Is(err, nats.ErrKeyNotFound) {
		return &model.UserTasks{Id: []string{}}, nil
	}
	if err != nil {
		return nil, err
	}
	return ut, nil
}

func (s *SharServer) getUserTask(ctx context.Context, req *model.GetUserTaskRequest) (*model.GetUserTaskResponse, error) {
	job, err := s.ns.GetJob(ctx, req.TrackingId)
	if err != nil {
		return nil, err
	}
	wf, err := s.ns.GetWorkflow(ctx, job.WorkflowId)
	if err != nil {
		return nil, err
	}
	els := make(map[string]*model.Element)
	for _, v := range wf.Process {
		common.IndexProcessElements(v.Elements, els)
	}
	return &model.GetUserTaskResponse{
		TrackingId:  job.Id,
		Owner:       req.Owner,
		Name:        els[job.ElementId].Name,
		Description: els[job.ElementId].Documentation,
		Vars:        job.Vars,
	}, nil
}

func (s *SharServer) handleWorkflowError(ctx context.Context, req *model.HandleWorkflowErrorRequest) (*model.HandleWorkflowErrorResponse, error) {
	// Sanity check
	if req.ErrorCode == "" {
		return nil, errors.New("ErrorCode may not be empty")
	}

	// First get the job that the error occurred in
	job, err := s.ns.GetJob(ctx, req.TrackingId)
	if err != nil {
		return nil, err
	}

	// Get the workflow, so we can look up the error definitions
	wf, err := s.ns.GetWorkflow(ctx, job.WorkflowId)
	if err != nil {
		return nil, err
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
		_, err := s.cancelWorkflowInstance(ctx, &model.CancelWorkflowInstanceRequest{Id: job.WorkflowInstanceId, State: model.CancellationState_Errored})
		if err != nil {
			return nil, fmt.Errorf("workflow-fatal: can't handle error code %s as the workflow doesn't support it, and failed to cancel the workflow: %w", req.ErrorCode, err)
		}
		return nil, fmt.Errorf("workflow-fatal: can't handle error code %s as the workflow doesn't support it", req.ErrorCode)
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

	if err := s.ns.PublishWorkflowState(ctx, messages.WorkflowTraversalExecute, &model.WorkflowState{
		ElementType:        target.Type,
		ElementId:          target.Id,
		WorkflowId:         job.WorkflowId,
		WorkflowInstanceId: job.WorkflowInstanceId,
		Id:                 job.Id,
		ParentId:           job.ParentId,
		Vars:               job.Vars,
	}, 0); err != nil {
		s.log.Error("failed to publish workflow state", zap.Error(err))
		return nil, err
	}
	return &model.HandleWorkflowErrorResponse{Handled: true}, nil
}

func (s *SharServer) getServerInstanceStats(ctx context.Context, req *emptypb.Empty) (*model.WorkflowStats, error) {
	ret := *s.ns.WorkflowStats()
	return &ret, nil
}

func listen[T proto.Message, U proto.Message](con common.NatsConn, log *zap.Logger, panicRecovery bool, subList map[*nats.Subscription]struct{}, subject string, req T, fn func(ctx context.Context, req T) (U, error)) (*nats.Subscription, error) {
	sub, err := con.QueueSubscribe(subject, subject, func(msg *nats.Msg) {
		ctx := context.Background()
		if err := callAPI(ctx, panicRecovery, req, msg, fn); err != nil {
			log.Error("API call for "+subject+" failed", zap.Error(err))
		}
	})
	if err != nil {
		return nil, fmt.Errorf("failed to subscribe to %s: %w", subject, err)
	}
	subList[sub] = struct{}{}
	return sub, nil
}

func callAPI[T proto.Message, U proto.Message](ctx context.Context, panicRecovery bool, container T, msg *nats.Msg, fn func(ctx context.Context, req T) (U, error)) error {
	if panicRecovery {
		defer recoverAPIpanic(msg)
	}
	if err := proto.Unmarshal(msg.Data, container); err != nil {
		errorResponse(msg, codes.InvalidArgument, err.Error())
		return err
	}
	resMsg, err := fn(ctx, container)
	if err != nil {
		c := codes.Unknown
		if errors2.IsWorkflowFatal(err) {
			c = codes.Internal
		}
		errorResponse(msg, c, err.Error())
		return err
	}
	res, err := proto.Marshal(resMsg)
	if err != nil {
		errorResponse(msg, codes.InvalidArgument, err.Error())
		return err
	}
	if err := msg.Respond(res); err != nil {
		errorResponse(msg, codes.FailedPrecondition, err.Error())
		return err
	}
	return nil
}

func recoverAPIpanic(msg *nats.Msg) {
	if r := recover(); r != nil {
		errorResponse(msg, codes.Internal, r)
		fmt.Println("recovered from ", r)
	}
}

func errorResponse(m *nats.Msg, code codes.Code, msg any) {
	if err := m.Respond(apiError(code, msg)); err != nil {
		fmt.Println("failed to send error response: " + string(apiError(codes.Internal, msg)))
	}
}

func apiError(code codes.Code, msg any) []byte {
	return []byte(fmt.Sprintf("ERR_%d|%+v", code, msg))
}
