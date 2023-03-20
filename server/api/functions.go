package api

import (
	"context"
	"errors"
	"fmt"
	"github.com/nats-io/nats.go"
	"gitlab.com/shar-workflow/shar/common"
	"gitlab.com/shar-workflow/shar/common/ctxkey"
	"gitlab.com/shar-workflow/shar/common/logx"
	"gitlab.com/shar-workflow/shar/common/subj"
	"gitlab.com/shar-workflow/shar/model"
	errors2 "gitlab.com/shar-workflow/shar/server/errors"
	"gitlab.com/shar-workflow/shar/server/messages"
	"gitlab.com/shar-workflow/shar/server/vars"
	"google.golang.org/protobuf/types/known/emptypb"
	"google.golang.org/protobuf/types/known/wrapperspb"
)

func (s *SharServer) getProcessInstanceStatus(ctx context.Context, req *model.GetProcessInstanceStatusRequest) (*model.GetProcessInstanceStatusResult, error) {
	ps, err := s.ns.GetProcessInstanceStatus(ctx, req.Id)
	if err != nil {
		return nil, fmt.Errorf("getProcessInstanceStatus failed with: %w", err)
	}
	return &model.GetProcessInstanceStatusResult{ProcessState: ps}, fmt.Errorf("getProcessinstace status failed with: %w", err)
}

func (s *SharServer) listWorkflowInstanceProcesses(ctx context.Context, req *model.ListWorkflowInstanceProcessesRequest) (*model.ListWorkflowInstanceProcessesResult, error) {
	ctx, instance, err2 := s.authFromInstanceID(ctx, req.Id)
	if err2 != nil {
		return nil, fmt.Errorf("authorize %v: %w", ctx.Value(ctxkey.APIFunc), err2)
	}
	res, err := s.ns.ListWorkflowInstanceProcesses(ctx, instance.WorkflowInstanceId)
	if err != nil {
		return nil, fmt.Errorf("get workflow instance status: %w", err)
	}
	return &model.ListWorkflowInstanceProcessesResult{ProcessInstanceId: res}, nil
}

func (s *SharServer) listWorkflows(ctx context.Context, _ *emptypb.Empty) (*model.ListWorkflowsResponse, error) {
	ctx, err2 := s.authForNonWorkflow(ctx)
	if err2 != nil {
		return nil, fmt.Errorf("authorize %v: %w", ctx.Value(ctxkey.APIFunc), err2)
	}
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
			return nil, fmt.Errorf("list workflowsr: %w", err)
		}
	}
}

func (s *SharServer) sendMessage(ctx context.Context, req *model.SendMessageRequest) (*emptypb.Empty, error) {
	//TODO: how do we auth this?

	if err := s.ns.PublishMessage(ctx, req.Name, req.CorrelationKey, req.Vars); err != nil {
		return nil, fmt.Errorf("send message: %w", err)
	}
	return &emptypb.Empty{}, nil
}

func (s *SharServer) completeManualTask(ctx context.Context, req *model.CompleteManualTaskRequest) (*emptypb.Empty, error) {
	ctx, job, err2 := s.authFromJobID(ctx, req.TrackingId)
	if err2 != nil {
		return nil, fmt.Errorf("authorize %v: %w", ctx.Value(ctxkey.APIFunc), err2)
	}
	if err := s.engine.CompleteManualTask(ctx, job, req.Vars); err != nil {
		return nil, fmt.Errorf("complete manual task: %w", err)
	}
	return &emptypb.Empty{}, nil
}

func (s *SharServer) completeServiceTask(ctx context.Context, req *model.CompleteServiceTaskRequest) (*emptypb.Empty, error) {
	ctx, job, err2 := s.authFromJobID(ctx, req.TrackingId)
	if err2 != nil {
		return nil, fmt.Errorf("authorize %v: %w", ctx.Value(ctxkey.APIFunc), err2)
	}
	if err := s.engine.CompleteServiceTask(ctx, job, req.Vars); err != nil {
		return nil, fmt.Errorf("complete service task: %w", err)
	}
	return &emptypb.Empty{}, nil
}

func (s *SharServer) completeSendMessageTask(ctx context.Context, req *model.CompleteSendMessageRequest) (*emptypb.Empty, error) {
	ctx, job, err2 := s.authFromJobID(ctx, req.TrackingId)
	if err2 != nil {
		return nil, fmt.Errorf("authorize %v: %w", ctx.Value(ctxkey.APIFunc), err2)
	}
	if err := s.engine.CompleteSendMessageTask(ctx, job, req.Vars); err != nil {
		return nil, fmt.Errorf("complete send message task: %w", err)
	}
	return &emptypb.Empty{}, nil
}

func (s *SharServer) completeUserTask(ctx context.Context, req *model.CompleteUserTaskRequest) (*emptypb.Empty, error) {
	ctx, job, err2 := s.authFromJobID(ctx, req.TrackingId)
	if err2 != nil {
		return &emptypb.Empty{}, fmt.Errorf("authorize complete user task: %w", err2)
	}
	if err := s.engine.CompleteUserTask(ctx, job, req.Vars); err != nil {
		return nil, fmt.Errorf("complete user task: %w", err)
	}
	return &emptypb.Empty{}, nil
}

func (s *SharServer) storeWorkflow(ctx context.Context, wf *model.Workflow) (*wrapperspb.StringValue, error) {
	ctx, err2 := s.authForNamedWorkflow(ctx, wf.Name)
	if err2 != nil {
		return nil, fmt.Errorf("authorize complete user task: %w", err2)
	}
	res, err := s.engine.LoadWorkflow(ctx, wf)
	if err != nil {
		return nil, fmt.Errorf("store workflow: %w", err)
	}
	return &wrapperspb.StringValue{Value: res}, nil
}

func (s *SharServer) getServiceTaskRoutingID(ctx context.Context, taskName *wrapperspb.StringValue) (*wrapperspb.StringValue, error) {
	ctx, err2 := s.authForNonWorkflow(ctx)
	if err2 != nil {
		return nil, fmt.Errorf("authorize %v: %w", ctx.Value(ctxkey.APIFunc), err2)
	}
	res, err := s.ns.GetServiceTaskRoutingKey(ctx, taskName.Value)
	if err != nil {
		return nil, fmt.Errorf("get service task routing id: %w", err)
	}
	return &wrapperspb.StringValue{Value: res}, nil
}

func (s *SharServer) launchWorkflow(ctx context.Context, req *model.LaunchWorkflowRequest) (*model.LaunchWorkflowResponse, error) {
	ctx, err2 := s.authForNamedWorkflow(ctx, req.Name)
	if err2 != nil {
		return nil, fmt.Errorf("authorize complete user task: %w", err2)
	}
	wfiID, wfID, err := s.engine.Launch(ctx, req.Name, req.Vars)
	if err != nil {
		return nil, fmt.Errorf("launch workflow instance kv: %w", err)
	}
	return &model.LaunchWorkflowResponse{WorkflowId: wfID, InstanceId: wfiID}, nil
}

func (s *SharServer) cancelWorkflowInstance(ctx context.Context, req *model.CancelWorkflowInstanceRequest) (*emptypb.Empty, error) {
	ctx, instance, err2 := s.authFromInstanceID(ctx, req.Id)
	if err2 != nil {
		return nil, fmt.Errorf("authorize %v: %w", ctx.Value(ctxkey.APIFunc), err2)
	}
	// TODO: get working state here
	state := &model.WorkflowState{
		WorkflowInstanceId: instance.WorkflowInstanceId,
		State:              req.State,
		Error:              req.Error,
	}
	err := s.engine.CancelWorkflowInstance(ctx, state)
	if err != nil {
		return nil, fmt.Errorf("cancel workflow instance kv: %w", err)
	}
	return &emptypb.Empty{}, nil
}

func (s *SharServer) listWorkflowInstance(ctx context.Context, req *model.ListWorkflowInstanceRequest) (*model.ListWorkflowInstanceResponse, error) {
	ctx, err2 := s.authForNamedWorkflow(ctx, req.WorkflowName)
	if err2 != nil {
		return nil, fmt.Errorf("authorize complete user task: %w", err2)
	}
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
			return nil, fmt.Errorf("list workflow instancesr: %w", err)
		}
	}
}

func (s *SharServer) handleWorkflowError(ctx context.Context, req *model.HandleWorkflowErrorRequest) (*model.HandleWorkflowErrorResponse, error) {
	ctx, job, err2 := s.authFromJobID(ctx, req.TrackingId)
	if err2 != nil {
		return nil, fmt.Errorf("authorize %v: %w", ctx.Value(ctxkey.APIFunc), err2)
	}
	// Sanity check
	if req.ErrorCode == "" {
		return nil, fmt.Errorf("ErrorCode may not be empty: %w", errors2.ErrMissingErrorCode)
	}

	// Get the workflow, so we can look up the error definitions
	wf, err := s.ns.GetWorkflow(ctx, job.WorkflowId)
	if err != nil {
		return nil, fmt.Errorf("get workflow definition for handle workflow error: %w", err)
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
		// TODO: This always assumes service task.  Wrong!
		if err := s.ns.PublishWorkflowState(ctx, subj.NS(messages.WorkflowJobServiceTaskAbort, "default"), job); err != nil {
			return nil, fmt.Errorf("cencel job: %w", werr)
		}

		cancelState := common.CopyWorkflowState(job)
		cancelState.State = model.CancellationState_errored
		cancelState.Error = &model.Error{
			Id:   "UNKNOWN",
			Name: "UNKNOWN",
			Code: req.ErrorCode,
		}
		if err := s.engine.CancelWorkflowInstance(ctx, cancelState); err != nil {
			return nil, fmt.Errorf("cancel workflow instance: %w", werr)
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
		return nil, fmt.Errorf("get old state for handle workflow error: %w", err)
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
		WorkflowName:       wf.Name,
		ProcessInstanceId:  job.ProcessInstanceId,
		ProcessName:        job.ProcessName,
	}); err != nil {
		log := logx.FromContext(ctx)
		log.Error("publish workflow state", err)
		return nil, fmt.Errorf("publish traversal for handle workflow error: %w", err)
	}
	// TODO: This always assumes service task.  Wrong!
	if err := s.ns.PublishWorkflowState(ctx, messages.WorkflowJobServiceTaskAbort, &model.WorkflowState{
		ElementType:        target.Type,
		ElementId:          target.Id,
		WorkflowId:         job.WorkflowId,
		WorkflowInstanceId: job.WorkflowInstanceId,
		Id:                 job.Id,
		Vars:               job.Vars,
		WorkflowName:       wf.Name,
		ProcessInstanceId:  job.ProcessInstanceId,
		ProcessName:        job.ProcessName,
	}); err != nil {
		log := logx.FromContext(ctx)
		log.Error("publish workflow state", err)
		// We have already traversed so retunring an error here would be incorrect.
		// It would force reprocessing and possibly double traversing
		// TODO: develop an idempotent behaviour based upon hash nats message ids + deduplication
		return nil, fmt.Errorf("publish abort task for handle workflow error: %w", err)
	}
	return &model.HandleWorkflowErrorResponse{Handled: true}, nil
}

func (s *SharServer) listUserTaskIDs(ctx context.Context, req *model.ListUserTasksRequest) (*model.UserTasks, error) {
	ctx, err2 := s.authForNonWorkflow(ctx)
	if err2 != nil {
		return nil, fmt.Errorf("authorize %v: %w", ctx.Value(ctxkey.APIFunc), err2)
	}
	oid, err := s.ns.OwnerID(req.Owner)
	if err != nil {
		return nil, fmt.Errorf("get owner ID: %w", err)
	}
	ut, err := s.ns.GetUserTaskIDs(ctx, oid)
	if errors.Is(err, nats.ErrKeyNotFound) {
		return &model.UserTasks{Id: []string{}}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get user task IDs: %w", err)
	}
	return ut, nil
}

func (s *SharServer) getUserTask(ctx context.Context, req *model.GetUserTaskRequest) (*model.GetUserTaskResponse, error) {
	ctx, job, err2 := s.authFromJobID(ctx, req.TrackingId)
	if err2 != nil {
		return nil, fmt.Errorf("authorize %v: %w", ctx.Value(ctxkey.APIFunc), err2)
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

func (s *SharServer) getServerInstanceStats(ctx context.Context, _ *emptypb.Empty) (*model.WorkflowStats, error) {
	ctx, err2 := s.authForNonWorkflow(ctx)
	if err2 != nil {
		return nil, fmt.Errorf("authorize %v: %w", ctx.Value(ctxkey.APIFunc), err2)
	}
	ret := *s.ns.WorkflowStats()
	return &ret, nil
}

func (s *SharServer) getWorkflowVersions(ctx context.Context, req *model.GetWorkflowVersionsRequest) (*model.GetWorkflowVersionsResponse, error) {
	ctx, err2 := s.authForNamedWorkflow(ctx, req.Name)
	if err2 != nil {
		return nil, fmt.Errorf("authorize %v: %w", ctx.Value(ctxkey.APIFunc), err2)
	}
	ret, err := s.ns.GetWorkflowVersions(ctx, req.Name)
	if err != nil {
		return nil, fmt.Errorf("get workflow versions: %w", err)
	}
	return &model.GetWorkflowVersionsResponse{Versions: ret}, nil
}

func (s *SharServer) getWorkflow(ctx context.Context, req *model.GetWorkflowRequest) (*model.GetWorkflowResponse, error) {
	ctx, err2 := s.authForNonWorkflow(ctx)
	if err2 != nil {
		return nil, fmt.Errorf("authorize %v: %w", ctx.Value(ctxkey.APIFunc), err2)
	}
	ret, err := s.ns.GetWorkflow(ctx, req.Id)
	if err != nil {
		return nil, fmt.Errorf("get workflow: %w", err)
	}
	return &model.GetWorkflowResponse{Definition: ret}, nil
}

func (s *SharServer) getProcessHistory(ctx context.Context, req *model.GetProcessHistoryRequest) (*model.GetProcessHistoryResponse, error) {
	ctx, _, err := s.authFromProcessInstanceID(ctx, req.Id)
	if err != nil {
		return nil, fmt.Errorf("authorize %v: %w", ctx.Value(ctxkey.APIFunc), err)
	}
	ret, err := s.ns.GetProcessHistory(ctx, req.Id)
	if err != nil {
		return nil, fmt.Errorf("get process history: %w", err)
	}
	return &model.GetProcessHistoryResponse{Entry: ret}, nil
}
