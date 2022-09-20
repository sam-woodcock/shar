package vars

import (
	"bytes"
	"encoding/gob"
	"fmt"
	"gitlab.com/shar-workflow/shar/common/expression"
	"gitlab.com/shar-workflow/shar/model"
	"gitlab.com/shar-workflow/shar/server/errors"
	"go.uber.org/zap"
)

// Encode encodes the map of workflow variables into a go binary to be sent across the wire.
func Encode(log *zap.Logger, vars model.Vars) ([]byte, error) {
	var buf bytes.Buffer
	enc := gob.NewEncoder(&buf)
	if err := enc.Encode(vars); err != nil {
		msg := "failed to encode vars"
		log.Error(msg, zap.Any("vars", vars))
		return nil, fmt.Errorf(msg+": %w", &errors.ErrWorkflowFatal{Err: err})
	}
	return buf.Bytes(), nil
}

// Decode decodes a go binary object containing workflow variables.
func Decode(log *zap.Logger, vars []byte) (model.Vars, error) {
	ret := make(map[string]any)
	if vars == nil {
		return ret, nil
	}
	r := bytes.NewReader(vars)
	d := gob.NewDecoder(r)
	if err := d.Decode(&ret); err != nil {
		msg := "failed to decode vars"
		log.Error(msg, zap.Any("vars", vars), zap.Error(err))
		return nil, fmt.Errorf(msg+": %w", &errors.ErrWorkflowFatal{Err: err})
	}
	return ret, nil
}

func InputVars(log *zap.Logger, state *model.WorkflowState, el *model.Element) error {
	localVars := make(map[string]interface{})
	if el.InputTransform != nil {
		processVars, err := Decode(log, state.Vars)
		if err != nil {
			return err
		}
		for k, v := range el.InputTransform {
			res, err := expression.EvalAny(log, v, processVars)
			if err != nil {
				return err
			}
			localVars[k] = res
		}
		if st, ok := processVars["_varState"]; ok {
			localVars["_varState"] = st
		}
		b, err := Encode(log, localVars)
		if err != nil {
			return err
		}
		state.LocalVars = b
	}
	return nil
}

func OutputVars(log *zap.Logger, state *model.WorkflowState, el *model.Element) error {
	if el.OutputTransform != nil {
		localVars, err := Decode(log, state.LocalVars)
		if err != nil {
			return err
		}
		processVars, err := Decode(log, state.Vars)
		if err != nil {
			return err
		}
		for k, v := range el.OutputTransform {
			res, err := expression.EvalAny(log, v, localVars)
			if err != nil {
				return err
			}
			processVars[k] = res
		}
		if st, ok := localVars["_varState"]; ok {
			processVars["_varState"] = st
		}
		b, err := Encode(log, processVars)
		if err != nil {
			return err
		}
		state.Vars = b
		state.LocalVars = nil
	}
	return nil
}

func CheckVars(log *zap.Logger, state *model.WorkflowState, el *model.Element) error {
	if el.OutputTransform != nil {
		localVars, err := Decode(log, state.LocalVars)
		if err != nil {
			return err
		}
		for _, v := range el.OutputTransform {
			list, err := expression.GetVariables(v)
			if err != nil {
				return err
			}
			for i := range list {
				if _, ok := localVars[i]; !ok {
					return fmt.Errorf("expected output variable [%s] missing", i)
				}
			}
		}
	}
	return nil
}

func Set(log *zap.Logger, v []byte, key string, value interface{}) ([]byte, error) {
	vs, err := Decode(log, v)
	if err != nil {
		return nil, err
	}
	vs[key] = value
	v, err = Encode(log, vs)
	if err != nil {
		return nil, err
	}
	return v, nil
}
