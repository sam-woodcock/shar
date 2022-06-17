package services

import (
	"context"
)

type Logging interface {
}

type EventProcessorFunc func(ctx context.Context, workflowInstanceId, elementId, traversalId string, vars []byte) error
type CompleteJobProcessorFunc func(ctx context.Context, jobId string, vars []byte) error
