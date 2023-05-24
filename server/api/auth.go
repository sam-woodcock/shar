package api

import (
	"context"
	"fmt"
	"gitlab.com/shar-workflow/shar/common/ctxkey"
	"gitlab.com/shar-workflow/shar/common/header"
	"gitlab.com/shar-workflow/shar/model"
	errors2 "gitlab.com/shar-workflow/shar/server/errors"
)

func (s *SharServer) authenticate(ctx context.Context) (context.Context, header.Values, error) {
	val := ctx.Value(header.ContextKey).(header.Values)
	res, authErr := s.apiAuthNFn(ctx, &model.ApiAuthenticationRequest{Headers: val})
	if authErr != nil || !res.Authenticated {
		return context.Background(), header.Values{}, fmt.Errorf("authenticate: %w", errors2.ErrApiAuthNFail)
	}
	ctx = context.WithValue(ctx, ctxkey.SharUser, res.User)
	return ctx, val, nil
}

func (s *SharServer) authorize(ctx context.Context, workflowName string) (context.Context, error) {
	ctx, val, err := s.authenticate(ctx)
	if err != nil {
		return ctx, fmt.Errorf("authenticate: %w", errors2.ErrApiAuthNFail)
	}
	if s.apiAuthZFn == nil {
		return ctx, nil
	}
	if authRes, err := s.apiAuthZFn(ctx, &model.ApiAuthorizationRequest{
		Headers:      val,
		Function:     ctx.Value(ctxkey.APIFunc).(string),
		WorkflowName: workflowName,
		User:         ctx.Value(ctxkey.SharUser).(string),
	}); err != nil || !authRes.Authorized {
		return ctx, fmt.Errorf("authorize: %w", errors2.ErrApiAuthZFail)
	}
	return ctx, nil
}

func (s *SharServer) authFromJobID(ctx context.Context, trackingID string) (context.Context, *model.WorkflowState, error) {
	job, err := s.ns.GetJob(ctx, trackingID)
	if err != nil {
		return ctx, nil, fmt.Errorf("get job for authorization: %w", err)
	}
	wi, err := s.ns.GetWorkflowInstance(ctx, job.WorkflowInstanceId)
	if err != nil {
		return ctx, nil, fmt.Errorf("get workflow instance for authorization: %w", err)
	}
	ctx, auth := s.authorize(ctx, wi.WorkflowName)
	if auth != nil {
		return ctx, nil, fmt.Errorf("authorize: %w", &errors2.ErrWorkflowFatal{Err: auth})
	}
	return ctx, job, nil
}

func (s *SharServer) authFromInstanceID(ctx context.Context, instanceID string) (context.Context, *model.WorkflowInstance, error) {
	wi, err := s.ns.GetWorkflowInstance(ctx, instanceID)
	if err != nil {
		return ctx, nil, fmt.Errorf("get workflow instance for authorization: %w", err)
	}
	ctx, auth := s.authorize(ctx, wi.WorkflowName)
	if auth != nil {
		return ctx, nil, fmt.Errorf("authorize: %w", &errors2.ErrWorkflowFatal{Err: auth})
	}
	return ctx, wi, nil
}

func (s *SharServer) authFromProcessInstanceID(ctx context.Context, instanceID string) (context.Context, *model.ProcessInstance, error) {
	pi, err := s.ns.GetProcessInstance(ctx, instanceID)
	if err != nil {
		return ctx, nil, fmt.Errorf("get workflow instance for authorization: %w", err)
	}
	ctx, auth := s.authorize(ctx, pi.WorkflowName)
	if auth != nil {
		return ctx, nil, fmt.Errorf("authorize: %w", &errors2.ErrWorkflowFatal{Err: auth})
	}
	return ctx, pi, nil
}

func (s *SharServer) authForNonWorkflow(ctx context.Context) (context.Context, error) {
	ctx, auth := s.authorize(ctx, "")
	if auth != nil {
		return ctx, fmt.Errorf("authorize: %w", &errors2.ErrWorkflowFatal{Err: auth})
	}
	return ctx, nil
}

func (s *SharServer) authForNamedWorkflow(ctx context.Context, name string) (context.Context, error) {
	ctx, auth := s.authorize(ctx, name)
	if auth != nil {
		return ctx, fmt.Errorf("authorize: %w", &errors2.ErrWorkflowFatal{Err: auth})
	}
	return ctx, nil
}

func (s *SharServer) authForRawData(ctx context.Context) (context.Context, error) {
	ctx, auth := s.authorize(ctx, "**")
	if auth != nil {
		return ctx, fmt.Errorf("authorize: %w", &errors2.ErrWorkflowFatal{Err: auth})
	}
	return ctx, nil
}
