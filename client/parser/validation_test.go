package parser

import (
	"bytes"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"os"
	"testing"
)

func Test_MissingServiceTaskExecute(t *testing.T) {

	b, err := os.ReadFile("../../testdata/bad/missing-servicetask-definition.bpmn")
	require.NoError(t, err)
	_, err = Parse("TestWorkflow", bytes.NewBuffer(b))
	assert.ErrorAs(t, err, &errMissingServiceTaskDefinition)
}
