package workflow

import (
	"bytes"
	"gitlab.com/shar-workflow/shar/client/parser"
	"gitlab.com/shar-workflow/shar/model"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"os"
	"testing"
)

func setupTestWorkflow(t *testing.T, workflowName string) (*zap.Logger, *Engine, *MockNatsService, *model.Workflow) {
	svc := &MockNatsService{}
	log, _ := zap.NewDevelopment()
	eng, err := NewEngine(log, svc)

	b, err := os.ReadFile("../../testdata/" + workflowName)
	require.NoError(t, err)
	wf, err := parser.Parse("TestWorkflow", bytes.NewBuffer(b))
	require.NoError(t, err)

	return log, eng, svc, wf
}
