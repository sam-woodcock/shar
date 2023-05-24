package intTest

import (
	"context"
	"fmt"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gitlab.com/shar-workflow/shar/client"
	support "gitlab.com/shar-workflow/shar/integration-support"
	"gitlab.com/shar-workflow/shar/model"
	"gitlab.com/shar-workflow/shar/server/messages"
	"os"
	"testing"
	"time"
)

func TestEndEventError(t *testing.T) {
	tst := &support.Integration{}
	tst.Setup(t, nil, nil)
	defer tst.Teardown()

	// Create a starting context
	ctx := context.Background()

	// Dial shar
	cl := client.New()
	if err := cl.Dial(ctx, tst.NatsURL); err != nil {
		panic(err)
	}

	// Load BPMN workflow
	b, err := os.ReadFile("../../testdata/errors.bpmn")
	if err != nil {
		panic(err)
	}
	if _, err := cl.LoadBPMNWorkflowFromBytes(ctx, "TestEndEventError", b); err != nil {
		panic(err)
	}

	d := &testErrorEndEventHandlerDef{finished: make(chan struct{}), t: t}
	// Register a service task
	err = cl.RegisterServiceTask(ctx, "couldThrowError", d.mayFail3)
	require.NoError(t, err)
	err = cl.RegisterServiceTask(ctx, "fixSituation", d.fixSituation)
	require.NoError(t, err)

	err = cl.RegisterProcessComplete("Process_07lm3kx", d.processEnd)
	require.NoError(t, err)
	// Launch the workflow
	_, _, err = cl.LaunchWorkflow(ctx, "TestEndEventError", model.Vars{})
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

type testErrorEndEventHandlerDef struct {
	finished chan struct{}
	t        *testing.T
}

// A "Hello World" service task
func (d *testErrorEndEventHandlerDef) mayFail3(ctx context.Context, client client.JobClient, _ model.Vars) (model.Vars, error) {
	if err := client.Log(ctx, messages.LogInfo, -1, "service task completed successfully", nil); err != nil {
		return nil, fmt.Errorf("logging failed: %w", err)
	}
	return model.Vars{"success": true}, nil
}

// A "Hello World" service task
func (d *testErrorEndEventHandlerDef) fixSituation(_ context.Context, _ client.JobClient, vars model.Vars) (model.Vars, error) {
	fmt.Println("carried", vars["carried"])
	panic("this event should not fire")
}
func (d *testErrorEndEventHandlerDef) processEnd(ctx context.Context, vars model.Vars, wfError *model.Error, state model.CancellationState) {
	assert.Equal(d.t, "103", wfError.Code)
	assert.Equal(d.t, "CatastrophicError", wfError.Name)
	assert.Equal(d.t, model.CancellationState_completed, state)
	close(d.finished)
}
