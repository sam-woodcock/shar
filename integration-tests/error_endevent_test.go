package integration_tests

import (
	"context"
	"fmt"
	"github.com/stretchr/testify/assert"
	"gitlab.com/shar-workflow/shar/client"
	"gitlab.com/shar-workflow/shar/model"
	"go.uber.org/zap"
	"os"
	"testing"
)

func TestEndEventError(t *testing.T) {
	setup()
	defer teardown()
	// Create a starting context
	ctx := context.Background()

	// Create logger
	log, _ := zap.NewDevelopment()

	defer func() {
		if err := log.Sync(); err != nil {
			fmt.Println("could not sync log")
		}
	}()

	// Dial shar
	cl := client.New(log)
	if err := cl.Dial("nats://127.0.0.1:4459"); err != nil {
		panic(err)
	}

	// Load BPMN workflow
	b, err := os.ReadFile("../testdata/errors.bpmn")
	if err != nil {
		panic(err)
	}
	if _, err := cl.LoadBPMNWorkflowFromBytes(ctx, "HandleErrorsTest", b); err != nil {
		panic(err)
	}

	// Register a service task
	cl.RegisterServiceTask("couldThrowError", mayFail3)
	cl.RegisterServiceTask("fixSituation", fixSituation3)

	// A hook to watch for completion
	complete := make(chan *model.WorkflowInstanceComplete, 100)
	cl.RegisterWorkflowInstanceComplete(complete)

	// Launch the workflow
	wfiID, err := cl.LaunchWorkflow(ctx, "HandleErrorsTest", model.Vars{})
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
	assert.Equal(t, model.CancellationState_Errored, final.WorkflowState)
}

// A "Hello World" service task
func mayFail3(_ context.Context, vars model.Vars) (model.Vars, error) {
	fmt.Println("service task completed successfully")
	return model.Vars{"success": true}, nil
}

// A "Hello World" service task
func fixSituation3(_ context.Context, vars model.Vars) (model.Vars, error) {
	panic("this event should not fire")
}
