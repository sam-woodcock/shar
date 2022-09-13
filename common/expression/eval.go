package expression

import (
	"fmt"
	"github.com/antonmedv/expr"
	"github.com/antonmedv/expr/ast"
	"github.com/antonmedv/expr/parser"
	errors2 "gitlab.com/shar-workflow/shar/server/errors"
	"go.uber.org/zap"
	"strings"
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

func EvalAny(log *zap.Logger, exp string, vars map[string]interface{}) (retval interface{}, reterr error) { //nolint:ireturn
	exp = strings.TrimSpace(exp)
	if len(exp) == 0 {
		return "", nil
	}
	if exp[0] == '=' {
		exp = exp[1:]
	} else {
		return exp, nil
	}
	ex, err := expr.Compile(exp)
	if err != nil {
		return nil, fmt.Errorf(err.Error()+": %w", errors2.ErrWorkflowFatal{Err: err})
	}

	res, err := expr.Run(ex, vars)
	if err != nil {
		return nil, err
	}

	defer func() {
		if err := recover(); err != nil {
			errex := fmt.Errorf("error evaluating expression: %+v: %+v: %w", exp, err, errors2.ErrWorkflowFatal{Err: err.(error)})
			log.Error(errex.Error())
			retval = nil
			reterr = errex
		}
	}()

	return res, nil
}

func GetVariables(exp string) (map[string]struct{}, error) {
	ret := make(map[string]struct{})
	exp = strings.TrimSpace(exp)
	if len(exp) == 0 {
		return ret, nil
	}
	if exp[0] == '=' {
		exp = exp[1:]
	} else {
		return ret, nil
	}
	c, err := parser.Parse(exp)
	if err != nil {
		return nil, err
	}

	g := &variableWalker{v: make(map[string]struct{})}
	ast.Walk(&c.Node, g)
	return g.v, nil
}

type variableWalker struct {
	v map[string]struct{}
}

func (w *variableWalker) Enter(n *ast.Node) {
	switch t := (*n).(type) {
	case *ast.IdentifierNode:
		w.v[t.Value] = struct{}{}
	}
}

func (w *variableWalker) Exit(n *ast.Node) {}
