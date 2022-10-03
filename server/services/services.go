package services

import (
	"context"
	"gitlab.com/shar-workflow/shar/common"
	"gitlab.com/shar-workflow/shar/model"
)

type EventProcessorFunc func(ctx context.Context, newActivityID string, traversal *model.WorkflowState, traverseOnly bool) error
type CompleteActivityProcessorFunc func(ctx context.Context, activity *model.WorkflowState) error
type CompleteJobProcessorFunc func(ctx context.Context, job *model.WorkflowState) error
type MessageCompleteProcessorFunc func(ctx context.Context, state *model.WorkflowState) error
type TraversalFunc func(ctx context.Context, wfi *model.WorkflowInstance, trackingId common.TrackingID, outbound *model.Targets, el map[string]*model.Element, v []byte) error
type LaunchFunc func(ctx context.Context, state *model.WorkflowState) error
