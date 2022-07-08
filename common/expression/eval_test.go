package expression

import (
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gitlab.com/shar-workflow/shar/model"
	"go.uber.org/zap"
	"testing"
)

func TestPositive(t *testing.T) {
	log, err := zap.NewDevelopment()
	require.NoError(t, err)
	res, err := Eval[bool](log, "97 == 97", model.Vars{})
	assert.NoError(t, err)
	assert.Equal(t, true, res)
}

func TestNoVariable(t *testing.T) {
	log, err := zap.NewDevelopment()
	require.NoError(t, err)
	res, err := Eval[bool](log, "a == 4.5", model.Vars{})
	assert.NoError(t, err)
	assert.Equal(t, false, res)
}

func TestVariable(t *testing.T) {
	log, err := zap.NewDevelopment()
	require.NoError(t, err)
	res, err := Eval[bool](log, "a == 4.5", model.Vars{"a": 4.5})
	assert.NoError(t, err)
	assert.Equal(t, true, res)
}
