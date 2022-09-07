package errors

import (
	"errors"
)

var (
	ErrClosing                  = errors.New("SHAR server is shutting down")
	ErrWorkflowInstanceNotFound = errors.New("workflow instance not found")
	ErrWorkflowNotFound         = errors.New("workflow not found")
)

// ErrWorkflowFatal signifys that the workflow must terniate
type ErrWorkflowFatal struct {
	Err error
}

func (e ErrWorkflowFatal) Error() string {
	return e.Err.Error()
}

func IsWorkflowFatal(err error) bool {
	var wff *ErrWorkflowFatal
	return errors.As(err, &wff)
}
