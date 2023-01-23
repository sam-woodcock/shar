package intTest

import (
	"context"
	"fmt"
	"github.com/stretchr/testify/require"
	"gitlab.com/shar-workflow/shar/client"
	"gitlab.com/shar-workflow/shar/common/workflow"
	support "gitlab.com/shar-workflow/shar/integration-support"
	"gitlab.com/shar-workflow/shar/model"
	"gitlab.com/shar-workflow/shar/server/tools/tracer"
	"os"
	"testing"
)

func TestUnhandledError(t *testing.T) {
	tst := &support.Integration{}
	tst.Setup(t, nil, nil)
	defer tst.Teardown()
	sub := tracer.Trace(tst.NatsURL)
	defer sub.Drain()
	//sub := tracer.Trace(NatsURL)
	//defer sub.Drain()

	// Create a starting context
	ctx := context.Background()

	// Dial shar
	cl := client.New(client.WithEphemeralStorage(), client.WithConcurrency(10))
	if err := cl.Dial(tst.NatsURL); err != nil {
		panic(err)
	}

	// Load BPMN workflow
	b, err := os.ReadFile("../../testdata/errors.bpmn")
	if err != nil {
		panic(err)
	}
	if _, err := cl.LoadBPMNWorkflowFromBytes(ctx, "TestUnhandledError", b); err != nil {
		panic(err)
	}

	d := &testErrorUnhandledHandlerDef{}

	// Register a service task
	err = cl.RegisterServiceTask(ctx, "couldThrowError", d.mayFail)
	require.NoError(t, err)
	err = cl.RegisterServiceTask(ctx, "fixSituation", d.fixSituation)
	require.NoError(t, err)
	// A hook to watch for completion
	complete := make(chan *model.WorkflowInstanceComplete, 100)
	cl.RegisterWorkflowInstanceComplete(complete)

	// Launch the workflow
	wfiID, _, err := cl.LaunchWorkflow(ctx, "TestUnhandledError", model.Vars{})
	if err != nil {
		panic(err)
	}

	// Listen for service tasks
	go func() {
		err := cl.Listen(ctx)
		if err != nil {
			panic(err)
		}
	}()

	// wait for the workflow to complete
	tst.AwaitWorkflowComplete(t, complete, wfiID)
	tst.AssertCleanKV()
}

type testErrorUnhandledHandlerDef struct {
}

// A "Hello World" service task
func (d *testErrorUnhandledHandlerDef) mayFail(_ context.Context, _ client.JobClient, _ model.Vars) (model.Vars, error) {
	fmt.Println("Throw unhandled error")
	return model.Vars{"success": false}, workflow.Error{Code: "102"}
}

// A "Hello World" service task
func (d *testErrorUnhandledHandlerDef) fixSituation(_ context.Context, _ client.JobClient, vars model.Vars) (model.Vars, error) {
	fmt.Println("Fixing")
	fmt.Println("carried", vars["carried"])
	return model.Vars{}, nil
}
