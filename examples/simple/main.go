package main

import (
	"context"
	"fmt"
	"github.com/crystal-construct/shar/client"
	"github.com/crystal-construct/shar/model"
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
	cl.Dial(nats.DefaultURL)

	// Load BPMN workflow
	b, err := os.ReadFile("examples/simple/testdata/workflow.bpmn")
	if err != nil {
		panic(err)
	}
	if _, err := cl.LoadBMPNWorkflowFromBytes(ctx, b); err != nil {
		panic(err)
	}

	// Register a service task
	cl.RegisterServiceTask("SimpleProcess", simpleProcess)

	// Launch the workflow
	if _, err := cl.LaunchWorkflow(ctx, "WorkflowDemo", model.Vars{}); err != nil {
		panic(err)
	}

	// Listen for service tasks
	go func() {
		err := cl.Listen(ctx)
		if err != nil {
			panic(err)
		}
	}()
	time.Sleep(1 * time.Hour)
}

// A "Hello World" service task
func simpleProcess(ctx context.Context, vars model.Vars) (model.Vars, error) {
	fmt.Println("Hello World")
	return model.Vars{}, nil
}
