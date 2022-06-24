package services

import (
	"context"
	"gitlab.com/shar-workflow/shar/model"
)

type Storage interface {
	GetJob(ctx context.Context, id string) (*model.WorkflowState, error)
}
