package vars

import (
	"bytes"
	"encoding/gob"
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
		return nil, errors.NewErrWorkflowFatal(msg, err)
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
		return nil, errors.NewErrWorkflowFatal(msg, err)
	}
	return ret, nil
}
