package intTests

import (
	"context"
	"fmt"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gitlab.com/shar-workflow/shar/client"
	"gitlab.com/shar-workflow/shar/model"
	"gitlab.com/shar-workflow/shar/server/messages"
	"go.uber.org/zap"
	"os"
	"testing"
)

func TestEndEventError(t *testing.T) {
	tst := &integration{}
	tst.setup(t)
	defer tst.teardown()

	// Create a starting context
	ctx := context.Background()

	// Create logger
	log, _ := zap.NewDevelopment()

	// Dial shar
	cl := client.New(log)
	if err := cl.Dial(natsURL); err != nil {
		panic(err)
	}

	// Load BPMN workflow
	b, err := os.ReadFile("../testdata/errors.bpmn")
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
	wfiID, err := cl.LaunchWorkflow(ctx, "TestEndEventError", model.Vars{})
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

	// wait for the workflow to complete
	for i := range complete {
		if i.WorkflowInstanceId == wfiID {
			final = i
			break
		}
	}
	assert.Equal(t, "103", final.Error.Code)
	assert.Equal(t, "CatastrophicError", final.Error.Name)
	assert.Equal(t, model.CancellationState_errored, final.WorkflowState)
	tst.AssertCleanKV()
}

type testErrorEndEventHandlerDef struct {
}

// A "Hello World" service task
func (d *testErrorEndEventHandlerDef) mayFail3(ctx context.Context, client *client.JobClient, _ model.Vars) (model.Vars, error) {
	if err := client.Log(ctx, messages.LogInfo, -1, "service task completed successfully", nil); err != nil {
		return nil, err
	}
	return model.Vars{"success": true}, nil
}

// A "Hello World" service task
func (d *testErrorEndEventHandlerDef) fixSituation(_ context.Context, _ *client.JobClient, vars model.Vars) (model.Vars, error) {
	fmt.Println("carried", vars["carried"])
	panic("this event should not fire")
}
