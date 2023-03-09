package intTest

import (
	"context"
	"errors"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gitlab.com/shar-workflow/shar/client"
	"gitlab.com/shar-workflow/shar/common/workflow"
	support "gitlab.com/shar-workflow/shar/integration-support"
	"gitlab.com/shar-workflow/shar/model"
	"os"
	"testing"
	"time"
)

func TestHandledError(t *testing.T) {
	tst := &support.Integration{}
	tst.Setup(t, nil, nil)
	defer tst.Teardown()

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

	d := errorHandledHandlerDef{tst: tst, finished: make(chan struct{})}

	// Register a service task
	err = cl.RegisterServiceTask(ctx, "couldThrowError", d.mayFail)
	require.NoError(t, err)
	err = cl.RegisterServiceTask(ctx, "fixSituation", d.fixSituation)
	require.NoError(t, err)

	// A hook to watch for completion
	err = cl.RegisterProcessComplete("Process_07lm3kx", d.processEnd)
	require.NoError(t, err)
	// Launch the workflow
	_, _, err = cl.LaunchWorkflow(ctx, "TestHandleError", model.Vars{})
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
	support.WaitForChan(t, d.finished, 20*time.Second)
	tst.AssertCleanKV()
}

type errorHandledHandlerDef struct {
	fixed    bool
	tst      *support.Integration
	finished chan struct{}
}

// A "Hello World" service task
func (d *errorHandledHandlerDef) mayFail(_ context.Context, _ client.JobClient, _ model.Vars) (model.Vars, error) {
	//Throw handled error
	return model.Vars{"success": false, "myVar": 69}, workflow.Error{Code: "101", WrappedError: errors.New("things went badly")}
}

// A "Hello World" service task
func (d *errorHandledHandlerDef) fixSituation(_ context.Context, _ client.JobClient, vars model.Vars) (model.Vars, error) {
	assert.Equal(d.tst.Test, 69, vars["testVal"])
	assert.Equal(d.tst.Test, 32768, vars["carried"])
	d.fixed = true
	return model.Vars{}, nil
}

func (d *errorHandledHandlerDef) processEnd(ctx context.Context, vars model.Vars, wfError *model.Error, state model.CancellationState) {
	close(d.finished)
}
