package errors

import (
	"errors"
)

var (
	ErrClosing                  = errors.New("SHAR server is shutting down")
	ErrWorkflowInstanceNotFound = errors.New("workflow instance not found")
	ErrWorkflowNotFound         = errors.New("workflow not found")
	ErrElementNotFound          = errors.New("element not found")
	ErrStateNotFound            = errors.New("state not found")
	ErrJobNotFound              = errors.New("job not found")
	ErrFatalBadDuration         = &ErrWorkflowFatal{Err: errors.New("timer embargo value could not be evaluated to an int or a duration")}
)

var (
	NatsMsgKeyNotFound = "nats: key not found"
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
