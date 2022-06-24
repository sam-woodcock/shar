package vars

import (
	"gitlab.com/shar-workflow/shar/model"
	"github.com/stretchr/testify/assert"
	"go.uber.org/zap"
	"testing"
)

func TestEncodeDecodeVars(t *testing.T) {
	v := make(model.Vars)
	log, _ := zap.NewDevelopment()
	v["first"] = 56
	v["second"] = "elvis"
	v["third"] = 5.98

	e, err := Encode(log, v)
	assert.NoError(t, err)
	d, err := Decode(log, e)
	assert.Equal(t, v["first"], d["first"])
	assert.Equal(t, v["second"], d["second"])
	assert.Equal(t, v["third"], d["third"])
}
