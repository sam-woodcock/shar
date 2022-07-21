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
	b, err := os.ReadFile("testdata/usertask.bpmn")
	if err != nil {
		panic(err)
	}
	if _, err := cl.LoadBPMNWorkflowFromBytes(ctx, "UserTaskWorkflowDemo", b); err != nil {
		panic(err)
	}

	// Register a service task
	cl.RegisterServiceTask("Prepare", prepare)
	cl.RegisterServiceTask("Complete", complete)

	// A hook to watch for completion
	complete := make(chan *model.WorkflowInstanceComplete, 100)
	cl.RegisterWorkflowInstanceComplete(complete)

	// Launch the workflow
	wfiID, err := cl.LaunchWorkflow(ctx, "UserTaskWorkflowDemo", model.Vars{"OrderId": 68})
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

	go func() {
		for {
			tsk, err := cl.ListUserTaskIDs(ctx, "andrei")
			if err == nil && tsk.Id != nil {
				cl.CompleteUserTask(ctx, "andrei", tsk.Id[0], model.Vars{"Forename": "Brangelina", "Surname": "Miggins"})
				return
			}
			time.Sleep(1 * time.Second)
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
func prepare(_ context.Context, vars model.Vars) (model.Vars, error) {
	fmt.Println("Preparing")
	oid := vars["OrderId"].(int)
	return model.Vars{"OrderId": oid + 1}, nil
}

// A "Hello World" service task
func complete(_ context.Context, vars model.Vars) (model.Vars, error) {
	fmt.Println("OrderId", vars["OrderId"])
	fmt.Println("Forename", vars["Forename"])
	fmt.Println("Surname", vars["Surname"])
	return model.Vars{}, nil
}
