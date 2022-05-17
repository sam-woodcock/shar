package errors

import "github.com/pkg/errors"

var (
	ErrClosing                  = errors.New("grpc server is shutting down")
	ErrWorkflowInstanceNotFound = errors.New("workflow instance not found")
	ErrWorkflowNotFound         = errors.New("workflow not found")
)
