package intTest

import (
	"context"
	"fmt"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gitlab.com/shar-workflow/shar/client"
	support "gitlab.com/shar-workflow/shar/integration-support"
	"gitlab.com/shar-workflow/shar/model"
	"gitlab.com/shar-workflow/shar/server/tools/tracer"
	"os"
	"testing"
)

func TestSimple(t *testing.T) {
	tst := &support.Integration{}
	tst.Setup(t, nil, nil)
	defer tst.Teardown()
	sub := tracer.Trace(tst.NatsURL)
	defer sub.Drain()
	// Create a starting context
	ctx := context.Background()

	// Dial shar
	cl := client.New(client.WithEphemeralStorage(), client.WithConcurrency(10))
	err := cl.Dial(tst.NatsURL)
	require.NoError(t, err)

	// Load BPMN workflow
	b, err := os.ReadFile("../../testdata/simple-workflow.bpmn")
	require.NoError(t, err)

	_, err = cl.LoadBPMNWorkflowFromBytes(ctx, "SimpleWorkflowTest", b)
	require.NoError(t, err)

	complete := make(chan *model.WorkflowInstanceComplete, 100)

	d := &testSimpleHandlerDef{t: t}

	// Register a service task
	cl.RegisterWorkflowInstanceComplete(complete)
	err = cl.RegisterServiceTask(ctx, "SimpleProcess", d.integrationSimple)
	require.NoError(t, err)

	// Launch the workflow
	wfiID, _, err := cl.LaunchWorkflow(ctx, "SimpleWorkflowTest", model.Vars{})
	require.NoError(t, err)
	// Listen for service tasks
	go func() {
		err := cl.Listen(ctx)
		require.NoError(t, err)
	}()
	tst.AwaitWorkflowComplete(t, complete, wfiID)
	tst.AssertCleanKV()
}

type testSimpleHandlerDef struct {
	t *testing.T
}

func (d *testSimpleHandlerDef) integrationSimple(_ context.Context, _ client.JobClient, vars model.Vars) (model.Vars, error) {
	fmt.Println("Hi")
	assert.Equal(d.t, 32768, vars["carried"].(int))
	vars["Success"] = true
	return vars, nil
}
