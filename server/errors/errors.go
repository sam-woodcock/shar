package errors

import "errors"

var (
	ErrClosing                  = errors.New("grpc server is shutting down")
	ErrWorkflowInstanceNotFound = errors.New("workflow instance not found")
	ErrWorkflowNotFound         = errors.New("workflow not found")
)

type ErrWorkflowFatal struct {
	Msg          string
	wrappedError error
}

func NewErrWorkflowFatal(msg string, err error) *ErrWorkflowFatal {
	return &ErrWorkflowFatal{Msg: msg, wrappedError: err}
}

func (e *ErrWorkflowFatal) Error() string {
	return e.Msg
}

func (e *ErrWorkflowFatal) Unwrap() error {
	return e.wrappedError
}
