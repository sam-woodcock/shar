package workflow

import (
	"bytes"
	"github.com/stretchr/testify/require"
	"gitlab.com/shar-workflow/shar/client/parser"
	"gitlab.com/shar-workflow/shar/model"

	"os"
	"testing"
)

func setupTestWorkflow(t *testing.T, workflowName string) (*Engine, *MockNatsService, *model.Workflow) {
	svc := &MockNatsService{}

	eng, err := New(svc)
	require.NoError(t, err)
	b, err := os.ReadFile("../../testdata/" + workflowName)
	require.NoError(t, err)
	wf, err := parser.Parse("TestWorkflow", bytes.NewBuffer(b))
	require.NoError(t, err)

	return eng, svc, wf
}
