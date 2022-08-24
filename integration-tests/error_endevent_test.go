package main

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
	if _, err := cl.LoadBPMNWorkflowFromBytes(ctx, "TestEndEventError", b); err != nil {
		panic(err)
	}

	d := &testErrorEndEventHandlerDef{}
	// Register a service task
	cl.RegisterServiceTask("couldThrowError", d.mayFail3)
	cl.RegisterServiceTask("fixSituation", d.fixSituation)

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
	assert.Equal(t, model.CancellationState_Errored, final.WorkflowState)
}

type testErrorEndEventHandlerDef struct {
}

// A "Hello World" service task
func (d *testErrorEndEventHandlerDef) mayFail3(_ context.Context, vars model.Vars) (model.Vars, error) {
	fmt.Println("service task completed successfully")
	return model.Vars{"success": true}, nil
}

// A "Hello World" service task
func (d *testErrorEndEventHandlerDef) fixSituation(_ context.Context, vars model.Vars) (model.Vars, error) {
	panic("this event should not fire")
}
