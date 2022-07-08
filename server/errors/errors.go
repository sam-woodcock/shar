package errors

import "errors"

var (
	ErrClosing                  = errors.New("grpc server is shutting down")
	ErrWorkflowInstanceNotFound = errors.New("workflow instance not found")
	ErrWorkflowNotFound         = errors.New("workflow not found")
)

var ErrWorkflowFatal = errors.New("a fatal workflow error occurred, workflow instance terminating")
