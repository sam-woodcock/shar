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

	// Create a api provider
	store, err := services.NewNatsClientProvider(log, nats.DefaultURL, nats.MemoryStorage)
	if err != nil {
		panic(err)
	}

	// Dial shar
	cl := client.New(store, log, "localhost:50000", nil)
	if err := cl.Dial(); err != nil {
		log.Fatal(err.Error())
	}

	// Load BPMN workflow
	if _, err := cl.LoadBPMNWorkflowFromFile(ctx, "examples/simple/testdata/workflow.bpmn"); err != nil {
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
