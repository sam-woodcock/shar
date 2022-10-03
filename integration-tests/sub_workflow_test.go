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
	"time"
)

func TestSubWorkflow(t *testing.T) {
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

	// Load BPMN workflows
	w1, err := os.ReadFile("../testdata/sub-workflow-parent.bpmn")
	require.NoError(t, err)
	w2, err := os.ReadFile("../testdata/sub-workflow-child.bpmn")
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
	select {
	case c := <-complete:
		fmt.Println("completed " + c.WorkflowInstanceId)
	case <-time.After(3 * time.Second):
		assert.Fail(t, "Timed out")
	}

}

type testSubWorkflowHandlerDef struct {
}

func (d *testSubWorkflowHandlerDef) afterCallingSubProcess(_ context.Context, vars model.Vars) (model.Vars, error) {
	fmt.Println(vars["x"])
	return model.Vars{}, nil
}

func (d *testSubWorkflowHandlerDef) duringSubProcess(_ context.Context, vars model.Vars) (model.Vars, error) {
	x := vars["z"].(int)
	return model.Vars{"z": x + 41}, nil
}

func (d *testSubWorkflowHandlerDef) beforeCallingSubProcess(_ context.Context, _ model.Vars) (model.Vars, error) {
	return model.Vars{"x": 1}, nil
}
