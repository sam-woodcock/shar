package main

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

func TestUnhandledError(t *testing.T) {
	//	if os.Getenv("INT_TEST") != "true" {
	//		t.Skip("Skipping integration test " + t.Name())
	//	}

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
	if _, err := cl.LoadBPMNWorkflowFromBytes(ctx, "TestUnhandledError", b); err != nil {
		panic(err)
	}

	d := &testErrorUnhandledHandlerDef{}

	// Register a service task
	cl.RegisterServiceTask("couldThrowError", d.mayFail)
	cl.RegisterServiceTask("fixSituation", d.fixSituation)

	// A hook to watch for completion
	complete := make(chan *model.WorkflowInstanceComplete, 100)
	cl.RegisterWorkflowInstanceComplete(complete)

	// Launch the workflow
	wfiID, err := cl.LaunchWorkflow(ctx, "TestUnhandledError", model.Vars{})
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
			fmt.Println("state", i.WorkflowState)
			break
		}
	}
}

type testErrorUnhandledHandlerDef struct {
}

// A "Hello World" service task
func (d *testErrorUnhandledHandlerDef) mayFail(_ context.Context, vars model.Vars) (model.Vars, error) {
	fmt.Println("Throw unhandled error")
	return model.Vars{"success": false}, workflow.Error{Code: "102"}
}

// A "Hello World" service task
func (d *testErrorUnhandledHandlerDef) fixSituation(_ context.Context, vars model.Vars) (model.Vars, error) {
	fmt.Println("Fixing")
	return model.Vars{}, nil
}
