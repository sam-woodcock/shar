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
```
