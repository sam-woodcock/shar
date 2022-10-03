package server

import "fmt"

type AbandonOpError struct {
	Err error
}

func (w *AbandonOpError) Error() string {
	return fmt.Sprintf("abandon operation: %v", w.Err)
}

func abandon(err error) *AbandonOpError {
	return &AbandonOpError{
		Err: err,
	}
}
