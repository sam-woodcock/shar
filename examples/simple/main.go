package main

import (
	"context"
	"fmt"
	"github.com/nats-io/nats.go"
	"gitlab.com/shar-workflow/shar/client"
	"gitlab.com/shar-workflow/shar/model"

	"os"
)

var finished = make(chan struct{})

func main() {
	// Create a starting context
	ctx := context.Background()

	// Dial shar
	cl := client.New()
	if err := cl.Dial(nats.DefaultURL); err != nil {
		panic(err)
	}

	// Load BPMN workflow
	b, err := os.ReadFile("testdata/simple-workflow.bpmn")
	if err != nil {
		panic(err)
	}
	if _, err := cl.LoadBPMNWorkflowFromBytes(ctx, "SimpleWorkflowDemo", b); err != nil {
		panic(err)
	}

	// Register a service task
	err = cl.RegisterServiceTask(ctx, "SimpleProcess", simpleProcess)
	if err != nil {
		panic(err)
	}
	err = cl.RegisterProcessComplete("Process_03llwnm", processEnd)
	if err != nil {
		panic(err)
	}

	// A hook to watch for completion

	// Launch the workflow
	_, _, err = cl.LaunchWorkflow(ctx, "SimpleWorkflowDemo", model.Vars{})
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
	<-finished
}

// A "Hello World" service task
func simpleProcess(_ context.Context, _ client.JobClient, _ model.Vars) (model.Vars, error) {
	fmt.Println("Hello World")
	return model.Vars{}, nil
}

func processEnd(ctx context.Context, vars model.Vars, wfError *model.Error, state model.CancellationState) {
	finished <- struct{}{}
}
