package integration_tests

import (
	"context"
	"fmt"
	"gitlab.com/shar-workflow/shar/client"
	"gitlab.com/shar-workflow/shar/common/workflow"
	"gitlab.com/shar-workflow/shar/model"
	"go.uber.org/zap"
	"os"
	"testing"
)

func _TestHandleError(t *testing.T) {
	setup()
	defer teardown()
	// Create a starting context
	ctx := context.Background()

	// Create logger
	log, _ := zap.NewDevelopment()

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
	if _, err := cl.LoadBPMNWorkflowFromBytes(ctx, "TestHandleError", b); err != nil {
		panic(err)
	}

	// Register a service task
	cl.RegisterServiceTask("couldThrowError", mayFail)
	cl.RegisterServiceTask("fixSituation", fixSituation)

	// A hook to watch for completion
	complete := make(chan *model.WorkflowInstanceComplete, 100)
	cl.RegisterWorkflowInstanceComplete(complete)

	// Launch the workflow
	wfiID, err := cl.LaunchWorkflow(ctx, "TestHandleError", model.Vars{})
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
	for i := range complete {
		if i.WorkflowInstanceId == wfiID {
			break
		}
	}
}

// A "Hello World" service task
func mayFail(_ context.Context, vars model.Vars) (model.Vars, error) {
	fmt.Println("Throw handled error")
	return model.Vars{"success": false}, workflow.Error{Code: "101"}
}

// A "Hello World" service task
func fixSituation(_ context.Context, vars model.Vars) (model.Vars, error) {
	fmt.Println("Fixing")
	return model.Vars{}, nil
}
