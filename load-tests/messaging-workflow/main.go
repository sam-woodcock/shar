package main

import (
	"context"
	"fmt"
	"github.com/nats-io/nats.go"
	"gitlab.com/shar-workflow/shar/client"
	"gitlab.com/shar-workflow/shar/model"
	"log"
	"os"
	"sync"
	"time"
)

var finished = make(chan struct{})

func main() {
	// Create a starting context
	ctx := context.Background()

	// Dial shar
	cl1 := client.New()

	if err := cl1.Dial(ctx, nats.DefaultURL); err != nil {
		panic(err)
	}

	// Load first workflow
	b, err := os.ReadFile("testdata/message-workflow.bpmn")
	if err != nil {
		panic(err)
	}
	if _, err := cl1.LoadBPMNWorkflowFromBytes(ctx, "MessageLoad", b); err != nil {
		panic(err)
	}

	// Register a service task
	err = cl1.RegisterServiceTask(ctx, "step1", step1)
	if err != nil {
		panic(err)
	}
	err = cl1.RegisterServiceTask(ctx, "step2", step2)
	if err != nil {
		panic(err)
	}
	err = cl1.RegisterMessageSender(ctx, "MessageLoad", "continueMessage", sendMessage)
	if err != nil {
		panic(err)
	}

	// A hook to watch for completion
	err = cl1.RegisterProcessComplete("Process_03llwnm", processEnd)
	if err != nil {
		panic(err)
	}

	// Listen for service tasks
	go func() {
		err := cl1.Listen(ctx)
		if err != nil {
			panic(err)
		}
	}()

	sw := time.Now()
	wg := sync.WaitGroup{}
	n := 5000
	for i := 0; i < n; i++ {
		// Launch the workflows
		// Launch the workflow
		if _, _, err := cl1.LaunchWorkflow(ctx, "MessageLoad", model.Vars{"orderId": 57}); err != nil {
			log.Fatal(err.Error())
			return
		}
		wg.Add(1)
	}
	// wait for the workflow to complete
	for i := 0; i < n; i++ {
		<-finished
		wg.Done()
	}
	wg.Wait()
	fmt.Println(-time.Until(sw))
}

func step1(_ context.Context, _ client.JobClient, _ model.Vars) (model.Vars, error) {
	fmt.Println("Step 1")
	return model.Vars{}, nil
}

func step2(_ context.Context, _ client.JobClient, _ model.Vars) (model.Vars, error) {
	fmt.Println("Step 2")
	return model.Vars{}, nil
}

func sendMessage(ctx context.Context, cmd client.MessageClient, _ model.Vars) error {
	fmt.Println("Sending Message...")
	if err := cmd.SendMessage(ctx, "continueMessage", 57, model.Vars{}); err != nil {
		return fmt.Errorf("send continue message: %w", err)
	}
	return nil
}

func processEnd(ctx context.Context, vars model.Vars, wfError *model.Error, state model.CancellationState) {
	finished <- struct{}{}
}
