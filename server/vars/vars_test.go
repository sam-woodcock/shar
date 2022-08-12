package vars

import (
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gitlab.com/shar-workflow/shar/model"
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
	require.NoError(t, err)
	d, err := Decode(log, e)
	require.NoError(t, err)
	assert.Equal(t, v["first"], d["first"])
	assert.Equal(t, v["second"], d["second"])
	assert.Equal(t, v["third"], d["third"])
}
