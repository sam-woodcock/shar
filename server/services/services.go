package services

import (
	"context"
	"github.com/crystal-construct/shar/model"
)

type Logging interface {
}

type EventProcessorFunc func(ctx context.Context, workflowInstanceId, elementId, traversalId string, vars []byte) error
type CompleteJobProcessorFunc func(ctx context.Context, jobId string, vars []byte) error
type MessageCompleteProcessorFunc func(ctx context.Context, state *model.WorkflowState) error
