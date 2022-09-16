package services

import (
	"context"
	"gitlab.com/shar-workflow/shar/model"
)

type EventProcessorFunc func(ctx context.Context, traversal *model.WorkflowState, traverseOnly bool) error
type CompleteJobProcessorFunc func(ctx context.Context, jobID string, vars []byte) error
type MessageCompleteProcessorFunc func(ctx context.Context, state *model.WorkflowState) error
type TraversalFunc func(ctx context.Context, wfi *model.WorkflowInstance, parentTrackingId string, outbound *model.Targets, el map[string]*model.Element, v []byte) error
