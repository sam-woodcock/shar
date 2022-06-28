package main

import (
	"context"
	"fmt"
	"github.com/nats-io/nats.go"
	"gitlab.com/shar-workflow/shar/client"
	"gitlab.com/shar-workflow/shar/model"
	"go.uber.org/zap"
	"os"
	"time"
)

var cl *client.Client

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
	cl = client.New(log)
	if err := cl.Dial(nats.DefaultURL); err != nil {
		panic(err)
	}

	// Load BPMN workflow
	b, err := os.ReadFile("testdata/message-manual-workflow.bpmn")
	if err != nil {
		panic(err)
	}
	if _, err := cl.LoadBPMNWorkflowFromBytes(ctx, "MessageManualDemo", b); err != nil {
		panic(err)
	}

	// Register a service task
	cl.RegisterServiceTask("step1", step1)
	cl.RegisterServiceTask("step2", step2)
	// Launch the workflow
	if _, err := cl.LaunchWorkflow(ctx, "MessageManualDemo", model.Vars{"orderId": 57}); err != nil {
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

func step1(ctx context.Context, vars model.Vars) (model.Vars, error) {
	fmt.Println("Step 1")
	fmt.Println("Sending Message...")
	cl.SendMessage(ctx, "", "continueMessage", 57, model.Vars{"success": 32768})
	return model.Vars{}, nil
}

func step2(ctx context.Context, vars model.Vars) (model.Vars, error) {
	fmt.Println("Step 2")
	fmt.Println(vars["success"])
	return model.Vars{}, nil
}
