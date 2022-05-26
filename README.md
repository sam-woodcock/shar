![Simple Hyperscale Activity Router](/shar.png?raw=true "SHAR")

## What is SHAR?
SHAR is a workflow engine powered by message queue.  It is capable of loading and executing BPMN workflow XML. 
It aims to be small, and simple and have a tiny footprint.

To accomplish massive scalability, the workflow transition, and activity calls are sent as immutable messages encapsulating their state.
SHAR uses a nats.io backend by default to facilitate redundancy and high throughput whilst still being able to run on low power hardware.

SHAR is 100% written in go, so takes advantage of the speed and size of a native executable.

## Why is SHAR?
Most BPMN engines are heavyweight and rely on proprietary storage and retry logic.
SHAR concentrates on being a workflow engine and lets reliable message queuing do the heavy lifting.

The developers of BPMN engines put a lot of work into making the persistence, scalability, resilience and retry logic for their products.
Messaging platforms such as nats.io have already tackled these challenges, and their dedicated solutions are usually more performant.

There is a tendency to write the engines in Java, which in turn requires a JVM to run.
Many give Go developers a native client to run workflows, but the engines remain a black box only extensible through Java.

## How do I use SHAR?
The following example assumes you have started the SHAR server. A [docker compose file](deploy/compose/docker-compose.yml) is provided to make this simple.

```go
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
	time.Sleep(1 * time.Second)
}

// A "Hello World" service task
func simpleProcess(ctx context.Context, vars model.Vars) (model.Vars, error) {
	fmt.Println("Hello World")
	return model.Vars{}, nil
}
```
If you want to trace the behaviour of the server you can check out the events in the [local Jaeger server](http://localhost:16686) included
