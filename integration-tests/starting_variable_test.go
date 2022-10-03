package intTests

import (
	"context"
	"fmt"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gitlab.com/shar-workflow/shar/client"
	"gitlab.com/shar-workflow/shar/model"
	"go.uber.org/zap"
	"os"
	"testing"
)

func TestStartingVariable(t *testing.T) {
	tst := &integration{}
	tst.setup(t)
	defer tst.teardown()

	// Create a starting context
	ctx := context.Background()

	// Create logger
	log, _ := zap.NewDevelopment()

	// Dial shar
	cl := client.New(log, client.WithEphemeralStorage())
	err := cl.Dial(natsURL)
	require.NoError(t, err)

	// Load BPMN workflow
	b, err := os.ReadFile("../testdata/bad/expects-starting-variable.bpmn")
	require.NoError(t, err)

	_, err = cl.LoadBPMNWorkflowFromBytes(ctx, "SimpleWorkflowTest", b)
	require.NoError(t, err)

	complete := make(chan *model.WorkflowInstanceComplete, 100)

	d := &testStartingVariableHandlerDef{}

	// Register a service task
	cl.RegisterWorkflowInstanceComplete(complete)
	err = cl.RegisterServiceTask(ctx, "DummyTask", d.integrationSimple)
	require.NoError(t, err)

	// Launch the workflow
	_, err = cl.LaunchWorkflow(ctx, "SimpleWorkflowTest", model.Vars{})

	assert.Error(t, err)

}

type testStartingVariableHandlerDef struct {
}

func (d *testStartingVariableHandlerDef) integrationSimple(_ context.Context, vars model.Vars) (model.Vars, error) {
	fmt.Println("Hi")
	return vars, nil
}
