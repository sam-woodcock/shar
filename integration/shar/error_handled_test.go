package intTest

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gitlab.com/shar-workflow/shar/client"
	"gitlab.com/shar-workflow/shar/common/workflow"
	support "gitlab.com/shar-workflow/shar/integration-support"
	"gitlab.com/shar-workflow/shar/model"
)

func TestHandledError(t *testing.T) {
	tst := &support.Integration{}
	tst.Setup(t, nil, nil)
	defer tst.Teardown()

	//sub := tracer.Trace("nats://127.0.0.1:4459")
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
	if _, err := cl.LoadBPMNWorkflowFromBytes(ctx, "TestHandleError", b); err != nil {
		panic(err)
	}

	d := errorHandledHandlerDef{tst: tst}

	// Register a service task
	err = cl.RegisterServiceTask(ctx, "couldThrowError", d.mayFail)
	require.NoError(t, err)
	err = cl.RegisterServiceTask(ctx, "fixSituation", d.fixSituation)
	require.NoError(t, err)

	// A hook to watch for completion
	complete := make(chan *model.WorkflowInstanceComplete, 100)
	cl.RegisterWorkflowInstanceComplete(complete)

	// Launch the workflow
	wfiID, _, err := cl.LaunchWorkflow(ctx, "TestHandleError", model.Vars{})
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

	finish := make(chan struct{})
	// wait for the workflow to complete
	go func() {
		for i := range complete {
			if i.WorkflowInstanceId == wfiID {
				close(finish)
				break
			}
		}
	}()
	select {
	case <-finish:
	case <-time.After(5 * time.Second):
		require.Fail(t, "timed out")
	}
	tst.AssertCleanKV()
}

type errorHandledHandlerDef struct {
	fixed bool
	tst   *support.Integration
}

// A "Hello World" service task
func (d *errorHandledHandlerDef) mayFail(_ context.Context, _ client.JobClient, _ model.Vars) (model.Vars, workflow.WrappedError) {
	//Throw handled error
	return model.Vars{"success": false, "myVar": 69}, workflow.Error{Code: "101", WrappedError: errors.New("things went badly")}
}

// A "Hello World" service task
func (d *errorHandledHandlerDef) fixSituation(_ context.Context, _ client.JobClient, vars model.Vars) (model.Vars, workflow.WrappedError) {
	assert.Equal(d.tst.Test, 69, vars["testVal"])
	assert.Equal(d.tst.Test, 32768, vars["carried"])
	d.fixed = true
	return model.Vars{}, nil
}
