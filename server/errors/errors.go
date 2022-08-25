package errors

import (
	"errors"
	"fmt"
)

var (
	ErrClosing                  = errors.New("SHAR server is shutting down")
	ErrWorkflowInstanceNotFound = errors.New("workflow instance not found")
	ErrWorkflowNotFound         = errors.New("workflow not found")
	ErrBadClientVersion         = errors.New("bad client version")
)

// ErrWorkflowFatal signifys that the workflow must terniate
type ErrWorkflowFatal struct {
	Err error
}

func (e ErrWorkflowFatal) Error() string {
	return fmt.Sprintf("%s", e.Err)
}
