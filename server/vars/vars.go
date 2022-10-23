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
		log.Error(msg, zap.Any("vars", vars), zap.Error(err))
		return nil, fmt.Errorf(msg+": %w", &errors.ErrWorkflowFatal{Err: err})
	}
	return buf.Bytes(), nil
}

// Decode decodes a go binary object containing workflow variables.
func Decode(log *zap.Logger, vars []byte) (model.Vars, error) {
	ret := make(map[string]any)
	if len(vars) == 0 {
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

// InputVars returns a set of variables matching an input requirement after transformation through expressions contained in an element.
func InputVars(log *zap.Logger, oldVarsBin []byte, newVarsBin *[]byte, el *model.Element) error {
	localVars := make(map[string]interface{})
	if el.InputTransform != nil {
		processVars, err := Decode(log, oldVarsBin)
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
		b, err := Encode(log, localVars)
		if err != nil {
			return err
		}
		*newVarsBin = b
	}
	return nil
}

// OutputVars merges one variable set into another based upon any expressions contained in an element.
func OutputVars(log *zap.Logger, newVarsBin []byte, mergeVarsBin *[]byte, transform map[string]string) error {
	if transform != nil {
		localVars, err := Decode(log, newVarsBin)
		if err != nil {
			return err
		}
		var processVars map[string]interface{}
		if mergeVarsBin == nil || len(*mergeVarsBin) > 0 {
			pv, err := Decode(log, *mergeVarsBin)
			if err != nil {
				return err
			}
			processVars = pv
		} else {
			processVars = make(map[string]interface{})
		}
		for k, v := range transform {
			res, err := expression.EvalAny(log, v, localVars)
			if err != nil {
				return err
			}
			processVars[k] = res
		}
		b, err := Encode(log, processVars)
		if err != nil {
			return err
		}
		*mergeVarsBin = b
	}
	return nil
}

// CheckVars checks for missing variables expected in a result
func CheckVars(log *zap.Logger, state *model.WorkflowState, el *model.Element) error {
	if el.OutputTransform != nil {
		vrs, err := Decode(log, state.Vars)
		if err != nil {
			return err
		}
		for _, v := range el.OutputTransform {
			list, err := expression.GetVariables(v)
			if err != nil {
				return err
			}
			for i := range list {
				if _, ok := vrs[i]; !ok {
					return errors.ErrWorkflowFatal{Err: fmt.Errorf("expected output variable [%s] missing", i)}
				}
			}
		}
	}
	return nil
}
