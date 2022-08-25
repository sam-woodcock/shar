package expression

import (
	"fmt"
	"github.com/antonmedv/expr"
	errors2 "gitlab.com/shar-workflow/shar/server/errors"
	"go.uber.org/zap"
)

func Eval[T any](log *zap.Logger, exp string, vars map[string]interface{}) (retval T, reterr error) { //nolint:ireturn
	ex, err := expr.Compile(exp)
	if err != nil {
		return *new(T), fmt.Errorf(err.Error()+": %w", errors2.ErrWorkflowFatal{Err: err})
	}

	res, err := expr.Run(ex, vars)
	if err != nil {
		return *new(T), err
	}

	defer func() {
		if err := recover(); err != nil {
			errex := fmt.Errorf("error evaluating expression: %+v: %+v: %w", exp, err, errors2.ErrWorkflowFatal{Err: err.(error)})
			log.Error(errex.Error())
			retval = *new(T)
			reterr = errex
		}
	}()

	return res.(T), nil
}
