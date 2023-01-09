package intTest

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gitlab.com/shar-workflow/shar/client"
	"gitlab.com/shar-workflow/shar/common/workflow"
	support "gitlab.com/shar-workflow/shar/integration-support"
	"gitlab.com/shar-workflow/shar/model"
	"gitlab.com/shar-workflow/shar/server/messages"
)

func TestEndEventError(t *testing.T) {
	tst := &support.Integration{}
	tst.Setup(t, nil, nil)
	defer tst.Teardown()

	// Create a starting context
	ctx := context.Background()

	// Dial shar
	cl := client.New()
	if err := cl.Dial(tst.NatsURL); err != nil {
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

	d := &testErrorEndEventHandlerDef{}
	// Register a service task
	err = cl.RegisterServiceTask(ctx, "couldThrowError", d.mayFail3)
	require.NoError(t, err)
	err = cl.RegisterServiceTask(ctx, "fixSituation", d.fixSituation)
	require.NoError(t, err)
	// A hook to watch for completion
	complete := make(chan *model.WorkflowInstanceComplete, 100)
	cl.RegisterWorkflowInstanceComplete(complete)

	// Launch the workflow
	wfiID, _, err := cl.LaunchWorkflow(ctx, "TestEndEventError", model.Vars{})
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

	var final *model.WorkflowInstanceComplete

	finish := make(chan struct{})
	// wait for the workflow to complete
	go func() {
		for i := range complete {
			if i.WorkflowInstanceId == wfiID {
				final = i
				close(finish)
				return
			}
		}
	}()
	select {
	case <-finish:
	case <-time.After(10 * time.Second):
		require.Fail(t, "timed out")
	}
	assert.Equal(t, "103", final.Error.Code)
	assert.Equal(t, "CatastrophicError", final.Error.Name)
	assert.Equal(t, model.CancellationState_errored, final.WorkflowState)
	tst.AssertCleanKV()
}

type testErrorEndEventHandlerDef struct {
}

// A "Hello World" service task
func (d *testErrorEndEventHandlerDef) mayFail3(ctx context.Context, client client.JobClient, _ model.Vars) (model.Vars, workflow.WrappedError) {
	if err := client.Log(ctx, messages.LogInfo, -1, "service task completed successfully", nil); err != nil {
		return nil, fmt.Errorf("logging failed: %w", err)
	}
	return model.Vars{"success": true}, nil
}

// A "Hello World" service task
func (d *testErrorEndEventHandlerDef) fixSituation(_ context.Context, _ client.JobClient, vars model.Vars) (model.Vars, workflow.WrappedError) {
	fmt.Println("carried", vars["carried"])
	panic("this event should not fire")
}
