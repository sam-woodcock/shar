package client

import "fmt"

// apiError returns a GRPC error code and an error message
type apiError struct {
	Code    int
	Message string
}

func (a apiError) Error() string {
	return fmt.Sprintf("code %d: %s", a.Code, a.Message)
}
