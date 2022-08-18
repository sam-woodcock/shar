package workflow

import (
	"fmt"
)

// Error describes a workflow error by code or name
type Error struct {
	Code         string
	WrappedError error
}

func (e Error) Error() string {
	return fmt.Sprintf("%s: %s", e.Code, e.WrappedError)
}
