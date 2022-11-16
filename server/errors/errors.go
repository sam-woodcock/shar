package errors

import (
	"errors"
)

var (
	ErrClosing                  = errors.New("SHAR server is shutting down")                                                               // ErrClosing signifies SHAR server is shutting dow
	ErrWorkflowInstanceNotFound = errors.New("workflow instance not found")                                                                // ErrWorkflowInstanceNotFound signifies workflow instance not found
	ErrWorkflowNotFound         = errors.New("workflow not found")                                                                         // ErrWorkflowNotFound signifies workflow not found
	ErrElementNotFound          = errors.New("element not found")                                                                          // ErrElementNotFound signifies element not found
	ErrStateNotFound            = errors.New("state not found")                                                                            // ErrStateNotFound signifies state not found
	ErrJobNotFound              = errors.New("job not found")                                                                              // ErrJobNotFound signifies job not found
	ErrFatalBadDuration         = &ErrWorkflowFatal{Err: errors.New("timer embargo value could not be evaluated to an int or a duration")} // ErrFatalBadDuration sigifies that the timer embargo value could not be evaluated to an int or a duration
)

var (
	NatsMsgKeyNotFound = "nats: key not found" // NatsMsgKeyNotFound is the substring match for NATS KV not found errors in
)

// ErrWorkflowFatal signifys that the workflow must terniate
type ErrWorkflowFatal struct {
	Err error
}

// Error returns the string version of the ErrWorkflowFatal error
func (e ErrWorkflowFatal) Error() string {
	return e.Err.Error()
}

// IsWorkflowFatal is a quick test to check whether the error contains ErrWorkflowFatal
func IsWorkflowFatal(err error) bool {
	var wff *ErrWorkflowFatal
	return errors.As(err, &wff)
}
