package main

import (
	"context"
	"fmt"
	"github.com/crystal-construct/shar/client"
	"github.com/crystal-construct/shar/client/services"
	"github.com/crystal-construct/shar/model"
	"github.com/nats-io/nats.go"
	"github.com/uptrace/opentelemetry-go-extra/otelzap"
	"go.uber.org/zap"
	"time"
)

func main() {
	// Create a starting context
	ctx := context.Background()

	// Create logger
	dev, _ := zap.NewDevelopment()

	// Wrap logger with open telemetry
	log := otelzap.New(dev, otelzap.WithMinLevel(-1))
	otelzap.ReplaceGlobals(log)
	defer func() {
		if err := log.Sync(); err != nil {
		}
	}()

	// Create a client provider
	store, err := services.NewNatsClientProvider(log, nats.DefaultURL, nats.MemoryStorage)
	if err != nil {
		panic(err)
	}

	// Dial shar
	cl := client.New(store, log, "localhost:50000", nil)
	if err := cl.Dial(); err != nil {
		log.Fatal(err.Error())
	}

	if _, err := cl.LoadBPMNWorkflowFromFile(ctx, "examples/sub-workflow/testdata/workflow.bpmn"); err != nil {
		panic(err)
	}
	if _, err := cl.LoadBPMNWorkflowFromFile(ctx, "examples/sub-workflow/testdata/subworkflow.bpmn"); err != nil {
		panic(err)
	}
	cl.RegisterServiceTask("BeforeCallingSubProcess", beforeCallingSubProcess)
	cl.RegisterServiceTask("DuringSubProcess", duringSubProcess)
	cl.RegisterServiceTask("AfterCallingSubProcess", afterCallingSubProcess)
	if _, err = cl.LaunchWorkflow(ctx, "WorkflowDemo", model.Vars{}); err != nil {
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
