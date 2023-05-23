package intTest

import (
	"context"
	"fmt"
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
	//tst.WithTrace = true
	tst.Setup(t, nil, nil)
	defer tst.Teardown()

	//sub := tracer.Trace(NatsURL)
	//defer sub.Drain()

	// Create a starting context
	ctx := context.Background()

	// Dial shar
	cl := client.New(client.WithEphemeralStorage(), client.WithConcurrency(10))
	err := cl.Dial(ctx, tst.NatsURL)
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

	d := &testSubWorkflowHandlerDef{finished: make(chan struct{})}

	// Register a service task
	err = cl.RegisterServiceTask(ctx, "BeforeCallingSubProcess", d.beforeCallingSubProcess)
	require.NoError(t, err)
	err = cl.RegisterServiceTask(ctx, "DuringSubProcess", d.duringSubProcess)
	require.NoError(t, err)
	err = cl.RegisterServiceTask(ctx, "AfterCallingSubProcess", d.afterCallingSubProcess)
	require.NoError(t, err)
	err = cl.RegisterProcessComplete("WorkflowDemo", d.processEnd)
	require.NoError(t, err)

	// Launch the workflow
	_, _, err = cl.LaunchWorkflow(ctx, "MasterWorkflowDemo", model.Vars{})
	require.NoError(t, err)
	// Listen for service tasks
	go func() {
		err := cl.Listen(ctx)
		require.NoError(t, err)
	}()
	support.WaitForChan(t, d.finished, 20*time.Second)
	tst.AssertCleanKV()
}

type testSubWorkflowHandlerDef struct {
	finished chan struct{}
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

func (d *testSubWorkflowHandlerDef) processEnd(ctx context.Context, vars model.Vars, wfError *model.Error, state model.CancellationState) {
	close(d.finished)
}
