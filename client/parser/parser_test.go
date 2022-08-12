package parser

import (
	"bytes"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"os"
	"testing"
)

//goland:noinspection GoNilness
func TestParseWorkflowDuration(t *testing.T) {
	b, err := os.ReadFile("../../testdata/test-timer-parse-duration.bpmn")
	require.NoError(t, err)
	p, err := Parse("Test", bytes.NewBuffer(b))
	require.NoError(t, err)
	assert.Equal(t, "2000000000", p.Process["Process_0cxoltv"].Elements[1].Execute)
}
