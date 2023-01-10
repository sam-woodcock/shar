package model_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"gitlab.com/shar-workflow/shar/model"
)

func TestVars(t *testing.T) {
	vars := model.Vars{"1": "value", "2": 77777.77777}

	s, err := vars.GetString("1")

	assert.NoError(t, err)
	assert.Equal(t, "value", s)

	f, err := vars.GetFloat64("2")

	assert.NoError(t, err)
	assert.Equal(t, f, 77777.77777)

	_, err = vars.GetInt("2")
	assert.Error(t, err)

	_, err = vars.GetBytes("3")
	assert.Error(t, err)
}
