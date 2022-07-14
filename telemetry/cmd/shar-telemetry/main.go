// Command jaeger is an example program that creates spans
// and uploads to Jaeger.
package main

import (
	"github.com/nats-io/nats.go"
	"gitlab.com/shar-workflow/shar/common"
	"gitlab.com/shar-workflow/shar/server/messages"
	"gitlab.com/shar-workflow/shar/telemetry/config"
	"gitlab.com/shar-workflow/shar/telemetry/server"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/exporters/jaeger"
	"go.opentelemetry.io/otel/sdk/resource"
	tracesdk "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.10.0"
	"go.uber.org/zap"
	"os"
	"time"
)

const (
	service     = "trace-demo"
	environment = "production"
	id          = 1
)

// tracerProvider returns an OpenTelemetry TracerProvider configured to use
// the Jaeger exporter that will send spans to the provided url. The returned
// TracerProvider will also use a Resource configured with all the information
// about the application.
func tracerProvider(url string) (*tracesdk.TracerProvider, error) {
	// Create the Jaeger exporter
	exp, err := jaeger.New(jaeger.WithCollectorEndpoint(jaeger.WithEndpoint(url)))
	if err != nil {
		return nil, err
	}

	tp := tracesdk.NewTracerProvider(
		// Always be sure to batch in production.
		tracesdk.WithBatcher(exp),
		// Record information about this application in a Resource.
		tracesdk.WithResource(resource.NewWithAttributes(
			semconv.SchemaURL,
			semconv.ServiceNameKey.String(service),
			attribute.String("environment", environment),
			attribute.Int64("ID", id),
		)),
	)
	return tp, nil
}

func main() {

	// Create a logger
	log, err := zap.NewDevelopment()
	if err != nil {
		panic(err)
	}

	// Get the configuration
	cfg, err := config.GetEnvironment()
	if err != nil {
		panic(err)
	}

	// Define our resource
	res := resource.NewWithAttributes(
		semconv.SchemaURL,
		semconv.ServiceNameKey.String(service),
		attribute.String("environment", environment),
		attribute.Int64("ID", id),
	)

	// Connect to nats
	nc, err := nats.Connect(cfg.NatsURL)
	if err != nil {
		panic(err)
	}

	// Get Jetstream
	js, err := nc.JetStream()
	if err != nil {
		panic(err)
	}

	if len(os.Args) > 1 && os.Args[1] == "--remove" {
		// Attempt both in case one failed last time, and deal with errors after
		err1 := js.DeleteConsumer("WORKFLOW", "Tracing")
		err2 := js.DeleteKeyValue(messages.KvTrace)
		if err1 != nil {
			panic(err1)
		}
		if err2 != nil {
			panic(err2)
		}
		return
	}

	// Create the Jaeger exporter
	exp, err := jaeger.New(jaeger.WithCollectorEndpoint(jaeger.WithEndpoint(cfg.JaegerURL)))
	if err != nil {
		panic(err)
	}

	if err := common.EnsureBuckets(js, nats.FileStorage, []string{"WORKFLOW_TRACE"}); err != nil {
		panic(err)
	}

	if err := common.EnsureConsumer(js, "WORKFLOW", &nats.ConsumerConfig{
		Durable:       "Tracing",
		Description:   "Sequential Trace Consumer",
		DeliverPolicy: nats.DeliverAllPolicy,
		FilterSubject: "WORKFLOW.State.>",
		AckPolicy:     nats.AckExplicitPolicy,
	}); err != nil {
		panic(err)
	}

	svr := server.New(js, log, res, exp)
	svr.Listen()
	time.Sleep(100 * time.Hour)
}
