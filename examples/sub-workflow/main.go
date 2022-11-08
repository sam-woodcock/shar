package main

import (
	"context"
	"fmt"
	"os"

	"github.com/nats-io/nats.go"
	"gitlab.com/shar-workflow/shar/client"
	"gitlab.com/shar-workflow/shar/model"
	"go.uber.org/zap"
)

func main() {
	// Create a starting context
	ctx := context.Background()

	// Create logger
	log, _ := zap.NewDevelopment()

	// Dial shar
	cl := client.New(log)
	if err := cl.Dial(nats.DefaultURL); err != nil {
		panic(err)
	}

	w1, _ := os.ReadFile("testdata/sub-workflow-parent.bpmn")
	w2, _ := os.ReadFile("testdata/sub-workflow-child.bpmn")
	if _, err := cl.LoadBPMNWorkflowFromBytes(ctx, "MasterWorkflowDemo", w1); err != nil {
		panic(err)
	}
	if _, err := cl.LoadBPMNWorkflowFromBytes(ctx, "SubWorkflowDemo", w2); err != nil {
		panic(err)
	}
	err := cl.RegisterServiceTask(ctx, "BeforeCallingSubProcess", beforeCallingSubProcess)
	if err != nil {
		panic(err)
	}
	err = cl.RegisterServiceTask(ctx, "DuringSubProcess", duringSubProcess)
	if err != nil {
		panic(err)
	}
	err = cl.RegisterServiceTask(ctx, "AfterCallingSubProcess", afterCallingSubProcess)
	if err != nil {
		panic(err)
	}

	// A hook to watch for completion
	complete := make(chan *model.WorkflowInstanceComplete, 100)
	cl.RegisterWorkflowInstanceComplete(complete)

	wfiID, err := cl.LaunchWorkflow(ctx, "MasterWorkflowDemo", model.Vars{})
	if err != nil {
		panic(err)
	}
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

func afterCallingSubProcess(_ context.Context, _ *client.JobClient, vars model.Vars) (model.Vars, error) {
	fmt.Println(vars["x"])
	return vars, nil
}

func duringSubProcess(_ context.Context, _ *client.JobClient, vars model.Vars) (model.Vars, error) {
	z := vars["z"].(int)
	return model.Vars{"z": z + 41}, nil
}

func beforeCallingSubProcess(_ context.Context, _ *client.JobClient, _ model.Vars) (model.Vars, error) {
	return model.Vars{"x": 1}, nil
}
