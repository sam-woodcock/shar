package errors

import (
	"errors"
	"fmt"
)

var (
	ErrClosing                  = errors.New("grpc server is shutting down")
	ErrWorkflowInstanceNotFound = errors.New("workflow instance not found")
	ErrWorkflowNotFound         = errors.New("workflow not found")
)

// ErrWorkflowFatal signifys that the workflow must terniate
type ErrWorkflowFatal struct {
	Err error
}

func (e ErrWorkflowFatal) Error() string {
	return fmt.Sprintf("%s", e.Err)
}
