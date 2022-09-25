package workflow

import (
	"bytes"
	"os"
	"testing"

	"github.com/stretchr/testify/require"
	"gitlab.com/shar-workflow/shar/client/parser"
	"gitlab.com/shar-workflow/shar/model"
	"go.uber.org/zap"
)

func setupTestWorkflow(t *testing.T, workflowName string) (*zap.Logger, *Engine, *MockNatsService, *model.Workflow) {
	svc := &MockNatsService{}
	log, _ := zap.NewDevelopment()
	eng := NewEngine(log, svc)
	b, err := os.ReadFile("../../testdata/" + workflowName)
	require.NoError(t, err)
	wf, err := parser.Parse("TestWorkflow", bytes.NewBuffer(b))
	require.NoError(t, err)

	return log, eng, svc, wf
}
