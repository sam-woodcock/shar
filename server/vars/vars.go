package vars

import (
	"bytes"
	"encoding/gob"
	"github.com/crystal-construct/shar/model"
	"go.uber.org/zap"
)

// encodeVars encodes the map of workflow variables into a go binary to be sent across the wire.
func Encode(log *zap.Logger, vars model.Vars) []byte {
	var buf bytes.Buffer
	enc := gob.NewEncoder(&buf)
	if err := enc.Encode(vars); err != nil {
		log.Error("failed to encode vars", zap.Any("vars", vars))
	}
	return buf.Bytes()
}

// decode decodes a go binary object containing workflow variables.
func Decode(log *zap.Logger, vars []byte) model.Vars {
	ret := make(map[string]interface{})
	if vars == nil {
		return ret
	}
	r := bytes.NewReader(vars)
	d := gob.NewDecoder(r)
	if err := d.Decode(&ret); err != nil {
		log.Error("failed to decode vars", zap.Any("vars", vars))
	}
	return ret
}
