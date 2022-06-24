package main

import (
	"context"
	"fmt"
	"gitlab.com/shar-workflow/shar/client"
	"gitlab.com/shar-workflow/shar/model"
	"github.com/nats-io/nats.go"
	"go.uber.org/zap"
	"os"
	"time"
)

func main() {
	// Create a starting context
	ctx := context.Background()

	// Create logger
	log, _ := zap.NewDevelopment()

	defer func() {
		if err := log.Sync(); err != nil {
		}
	}()

	// Dial shar
	cl := client.New(log)
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
	cl.RegisterServiceTask("SimpleProcess", simpleProcess)

	// Launch the workflow
	if _, err := cl.LaunchWorkflow(ctx, "SimpleWorkflowDemo", model.Vars{}); err != nil {
		panic(err)
	}

	// Listen for service tasks
	go func() {
		err := cl.Listen(ctx)
		if err != nil {
			panic(err)
		}
	}()
	time.Sleep(1 * time.Second)
}

// A "Hello World" service task
func simpleProcess(ctx context.Context, vars model.Vars) (model.Vars, error) {
	fmt.Println("Hello World")
	return model.Vars{}, nil
}
