package client

import "fmt"

//ApiError returns a GRPC error code and an error message
type ApiError struct {
	Code    int
	Message string
}

func (a ApiError) Error() string {
	return fmt.Sprintf("code %d: %s", a.Code, a.Message)
}
