package intTest

import (
	"context"
	"fmt"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gitlab.com/shar-workflow/shar/client"
	support "gitlab.com/shar-workflow/shar/integration-support"
	"gitlab.com/shar-workflow/shar/model"
	"os"
	"testing"
	"time"
)

func TestSubWorkflow(t *testing.T) {
	tst := &support.Integration{}
	tst.Setup(t, nil, nil)
	defer tst.Teardown()

	//sub := tracer.Trace(NatsURL)
	//defer sub.Drain()

	// Create a starting context
	ctx := context.Background()

	// Dial shar
	cl := client.New(client.WithEphemeralStorage(), client.WithConcurrency(10))
	err := cl.Dial(support.NatsURL)
	require.NoError(t, err)

	// Load BPMN workflows
	w1, err := os.ReadFile("../../testdata/sub-workflow-parent.bpmn")
	require.NoError(t, err)
	w2, err := os.ReadFile("../../testdata/sub-workflow-child.bpmn")
	require.NoError(t, err)

	_, err = cl.LoadBPMNWorkflowFromBytes(ctx, "MasterWorkflowDemo", w1)
	require.NoError(t, err)
	_, err = cl.LoadBPMNWorkflowFromBytes(ctx, "SubWorkflowDemo", w2)
	require.NoError(t, err)

	complete := make(chan *model.WorkflowInstanceComplete, 100)

	d := &testSubWorkflowHandlerDef{}

	// Register a service task
	cl.RegisterWorkflowInstanceComplete(complete)
	err = cl.RegisterServiceTask(ctx, "BeforeCallingSubProcess", d.beforeCallingSubProcess)
	require.NoError(t, err)
	err = cl.RegisterServiceTask(ctx, "DuringSubProcess", d.duringSubProcess)
	require.NoError(t, err)
	err = cl.RegisterServiceTask(ctx, "AfterCallingSubProcess", d.afterCallingSubProcess)
	require.NoError(t, err)

	// Launch the workflow
	if _, err := cl.LaunchWorkflow(ctx, "MasterWorkflowDemo", model.Vars{}); err != nil {
		panic(err)
	}

	// Listen for service tasks
	go func() {
		err := cl.Listen(ctx)
		require.NoError(t, err)
	}()
	for i := 0; i < 2; i++ {
		select {
		case c := <-complete:
			fmt.Println("completed " + c.WorkflowInstanceId)
		case <-time.After(3 * time.Second):
			assert.Fail(t, "Timed out")
		}
	}
	tst.AssertCleanKV()
}

type testSubWorkflowHandlerDef struct {
}

func (d *testSubWorkflowHandlerDef) afterCallingSubProcess(_ context.Context, _ client.JobClient, vars model.Vars) (model.Vars, error) {
	fmt.Println(vars["x"])
	fmt.Println("carried", vars["carried"])
	return model.Vars{}, nil
}

func (d *testSubWorkflowHandlerDef) duringSubProcess(_ context.Context, _ client.JobClient, vars model.Vars) (model.Vars, error) {
	x := vars["z"].(int)
	return model.Vars{"z": x + 41}, nil
}

func (d *testSubWorkflowHandlerDef) beforeCallingSubProcess(_ context.Context, _ client.JobClient, _ model.Vars) (model.Vars, error) {
	return model.Vars{"x": 1}, nil
}
