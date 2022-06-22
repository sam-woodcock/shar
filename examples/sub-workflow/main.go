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

	// Dial shar
	cl := client.New(log)
	cl.Dial(nats.DefaultURL)

	w1, _ := os.ReadFile("testdata/sub-workflow-parent.bpmn")
	w2, _ := os.ReadFile("testdata/sub-workflow-child.bpmn")
	if _, err := cl.LoadBPMNWorkflowFromBytes(ctx, "MasterWorkflowDemo", w1); err != nil {
		panic(err)
	}
	if _, err := cl.LoadBPMNWorkflowFromBytes(ctx, "SubWorkflowDemo", w2); err != nil {
		panic(err)
	}
	cl.RegisterServiceTask("BeforeCallingSubProcess", beforeCallingSubProcess)
	cl.RegisterServiceTask("DuringSubProcess", duringSubProcess)
	cl.RegisterServiceTask("AfterCallingSubProcess", afterCallingSubProcess)
	if _, err := cl.LaunchWorkflow(ctx, "MasterWorkflowDemo", model.Vars{}); err != nil {
		panic(err)
	}
	go func() {
		err := cl.Listen(ctx)
		if err != nil {
			panic(err)
		}
	}()
	time.Sleep(1 * time.Second)
}

func afterCallingSubProcess(ctx context.Context, vars model.Vars) (model.Vars, error) {
	fmt.Println(vars["x"])
	return model.Vars{}, nil
}

func duringSubProcess(ctx context.Context, vars model.Vars) (model.Vars, error) {
	x := vars["x"].(int)
	return model.Vars{"x": x + 41}, nil
}

func beforeCallingSubProcess(ctx context.Context, vars model.Vars) (model.Vars, error) {
	return model.Vars{"x": 1}, nil
}
