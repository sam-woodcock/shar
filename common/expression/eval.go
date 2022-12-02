package expression

import (
	"context"
	"fmt"
	"github.com/antonmedv/expr"
	"github.com/antonmedv/expr/ast"
	"github.com/antonmedv/expr/parser"
	"gitlab.com/shar-workflow/shar/common/logx"
	errors2 "gitlab.com/shar-workflow/shar/server/errors"
	"strings"
)

// Eval evaluates an expression given a set of variables and returns a generic type.
func Eval[T any](ctx context.Context, exp string, vars map[string]interface{}) (retval T, reterr error) { //nolint:ireturn
	if len(exp) == 0 {
		return *new(T), nil
	}
	if exp[0] == '=' {
		exp = exp[1:]
	}
	exp = strings.TrimPrefix(exp, "=")
	ex, err := expr.Compile(exp)
	if err != nil {
		return *new(T), fmt.Errorf(err.Error()+": %w", &errors2.ErrWorkflowFatal{Err: err})
	}

	res, err := expr.Run(ex, vars)
	if err != nil {
		return *new(T), fmt.Errorf("failed to evaluate expression: %w", err)
	}

	defer func() {
		if err := recover(); err != nil {
			retval = *new(T)
			reterr = logx.Err(ctx, "error evaluating expression", &errors2.ErrWorkflowFatal{Err: err.(error)}, "expression", exp)
		}
	}()

	return res.(T), nil
}

// EvalAny evaluates an expression given a set of variables and returns a 'boxed' interface type.
func EvalAny(ctx context.Context, exp string, vars map[string]interface{}) (retval interface{}, reterr error) { //nolint:ireturn
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
		return nil, logx.Err(ctx, "error compiling expression", &errors2.ErrWorkflowFatal{Err: err}, "expression", exp)
	}

	res, err := expr.Run(ex, vars)
	if err != nil {
		return nil, fmt.Errorf("failed during expression execution: %w", err)
	}

	defer func() {
		if err := recover(); err != nil {
			retval = nil
			reterr = logx.Err(ctx, "error evaluating expression", &errors2.ErrWorkflowFatal{Err: err.(error)}, "expression", exp)
		}
	}()

	return res, nil
}

// GetVariables returns a list of variables mentioned in an expression
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
		return nil, fmt.Errorf("get variables failed to parse expression %w", err)
	}

	g := &variableWalker{v: make(map[string]struct{})}
	ast.Walk(&c.Node, g)
	return g.v, nil
}

type variableWalker struct {
	v map[string]struct{}
}

// Enter is called from the visitor to iterate all IdentifierNode types
func (w *variableWalker) Enter(n *ast.Node) {
	switch t := (*n).(type) {
	case *ast.IdentifierNode:
		w.v[t.Value] = struct{}{}
	}
}

// Exit is unused in the variableWalker implementation
func (w *variableWalker) Exit(_ *ast.Node) {}
