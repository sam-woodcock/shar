package main

import (
	"bytes"
	"context"
	"fmt"
	"os"

	"github.com/nats-io/nats.go"
	"github.com/opentracing/opentracing-go"
	"github.com/uber/jaeger-client-go"
	"github.com/uber/jaeger-client-go/config"
	"gitlab.com/shar-workflow/shar/client"
	"gitlab.com/shar-workflow/shar/model"
	"go.uber.org/zap"
)

func main() {
	// Create a starting context
	ctx := context.Background()

	// Create logger
	log, _ := zap.NewDevelopment()

	defer func() {
		if err := log.Sync(); err != nil {
			fmt.Println(err)
		}
	}()

	// Dial shar
	cl := client.New(log)
	if err := cl.Dial(nats.DefaultURL); err != nil {
		panic(err)
	}

	// Load BPMN workflow
	b, err := os.ReadFile("testdata/simple-workflow-with-tracking.bpmn")
	if err != nil {
		panic(err)
	}
	if _, err := cl.LoadBPMNWorkflowFromBytes(ctx, "SimpleWorkflowDemo", b); err != nil {
		panic(err)
	}

	// Register a service task
	err = cl.RegisterServiceTask(ctx, "SimpleProcess", simpleProcess)
	if err != nil {
		panic(err)
	}
	// A hook to watch for completion
	complete := make(chan *model.WorkflowInstanceComplete, 100)
	cl.RegisterWorkflowInstanceComplete(complete)

	// Create a Jaeger Tracer Config
	cfg := &config.Configuration{
		ServiceName: "SimpleWorkflowDemo",
		Sampler: &config.SamplerConfig{
			Type:  "const",
			Param: 1,
		},
		Reporter: &config.ReporterConfig{
			LogSpans: true,
		},
	}

	// Start a new tracer
	tracer, closer, err := cfg.NewTracer(config.Logger(jaeger.StdLogger))
	if err != nil {
		panic(fmt.Sprintf("ERROR: cannot init Jaeger: %v\n", err))
	}
	defer closer.Close()

	// Start a new span
	var span = tracer.StartSpan("Launch Workflow")
	defer span.Finish()

	// Gather tracer kvp to pass on
	buf := bytes.NewBuffer(nil)
	tracer.Inject(span.Context(), opentracing.Binary, buf)

	// Launch the workflow
	wfiID, err := cl.LaunchWorkflow(ctx, "SimpleWorkflowDemo", model.Vars{"tracer": buf.Bytes()})
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

	// wait for the workflow to complete
	for i := range complete {
		if i.WorkflowInstanceId == wfiID {
			break
		}
	}
}

// A "Hello World" service task
func simpleProcess(_ context.Context, vars model.Vars) (model.Vars, error) {

	// Create a Jaeger Tracer Config
	cfg := &config.Configuration{
		ServiceName: "SimpleWorkflowDemo",
		Sampler: &config.SamplerConfig{
			Type:  "const",
			Param: 1,
		},
		Reporter: &config.ReporterConfig{
			LogSpans: true,
		},
	}

	// Start a new tracer
	tracer, closer, err := cfg.NewTracer(config.Logger(jaeger.StdLogger))
	if err != nil {
		panic(fmt.Sprintf("ERROR: cannot init Jaeger: %v\n", err))
	}
	defer closer.Close()

	// Start a new span
	spanCtx, err := tracer.Extract(opentracing.Binary, bytes.NewBuffer(vars["tracer"].([]byte)))
	if err != nil {
		fmt.Println(err)
	}

	span := tracer.StartSpan("simpleProcess", opentracing.FollowsFrom(spanCtx))
	defer span.Finish()

	fmt.Println("Hello World")
	return model.Vars{}, nil
}
