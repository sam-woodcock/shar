package workflow

import (
	"fmt"
)

// WrappedError describes a workflow error by code or name
type WrappedError interface {
	Error() string
}

// Error type for wrapped errors
type Error struct {
	Code         string
	WrappedError error
}

// Error returns the code and error as a string
func (e Error) Error() string {
	var we string
	if e.WrappedError != nil {
		we = e.WrappedError.Error()
	}
	return fmt.Sprintf("%s: %s", e.Code, we)
}
