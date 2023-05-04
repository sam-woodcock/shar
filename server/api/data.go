package api

import (
	"context"
	"fmt"
	"gitlab.com/shar-workflow/shar/model"
)

func (s *SharServer) spoolWorkflowEvents(ctx context.Context, req *model.SpoolWorkflowEventsRequest) (*model.SpoolWorkflowEventsResponse, error) {
	ctx, err2 := s.authForRawData(ctx)
	if err2 != nil {
		return nil, fmt.Errorf("authorize spool workflow events for raw data access: %w", err2)
	}
	res, err := s.ns.SpoolWorkflowEvents(ctx, 20)
	if err != nil {
		return nil, fmt.Errorf("spool workflow events kv: %w", err)
	}
	ret := make([]*model.WorkflowStateSummary, 0, 20)

	for _, s := range res {
		ret = append(ret, &model.WorkflowStateSummary{
			WorkflowId:         s.WorkflowId,
			WorkflowInstanceId: s.WorkflowInstanceId,
			ElementId:          s.ElementId,
			ElementType:        s.ElementType,
			Id:                 s.Id,
			Execute:            s.Execute,
			State:              s.State,
			Condition:          s.Condition,
			UnixTimeNano:       s.UnixTimeNano,
			Vars:               s.Vars,
			Error:              s.Error,
			Timer:              s.Timer,
			ProcessInstanceId:  s.ProcessInstanceId,
		})
	}

	return &model.SpoolWorkflowEventsResponse{
		State: ret,
	}, nil
}
