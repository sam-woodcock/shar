package vars

import (
	"bytes"
	"encoding/gob"
	"fmt"
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
		return nil, fmt.Errorf(msg+": %w", errors.ErrWorkflowFatal{Err: err})
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
		return nil, fmt.Errorf(msg+": %w", errors.ErrWorkflowFatal{Err: err})
	}
	return ret, nil
}

func Merge(log *zap.Logger, vars []byte, newVars []byte) ([]byte, error) {
	v1, err := Decode(log, vars)
	if err != nil {
		return nil, err
	}
	v2, err := Decode(log, newVars)
	if err != nil {
		return nil, err
	}
	for k, v := range v2 {
		v1[k] = v
	}
	return Encode(log, v1)
}
