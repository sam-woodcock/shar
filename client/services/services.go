package services

import (
	"context"
	"github.com/crystal-construct/shar/model"
)

type Storage interface {
	GetJob(ctx context.Context, id string) (*model.WorkflowState, error)
}
