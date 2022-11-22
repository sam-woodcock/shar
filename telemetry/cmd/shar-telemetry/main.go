package main

import (
	"context"
	"github.com/nats-io/nats.go"
	"gitlab.com/shar-workflow/shar/common"
	"gitlab.com/shar-workflow/shar/common/subj"
	"gitlab.com/shar-workflow/shar/server/messages"
	"gitlab.com/shar-workflow/shar/telemetry/config"
	"gitlab.com/shar-workflow/shar/telemetry/server"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/exporters/jaeger"
	"go.opentelemetry.io/otel/sdk/resource"
	semconv "go.opentelemetry.io/otel/semconv/v1.10.0"
	"os"
	"time"
)

const (
	service     = "shar"
	environment = "production"
	id          = 1
)

func main() {

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
		err2 := js.DeleteKeyValue(messages.KvTracking)
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

	if err := EnsureConsumer(js, "WORKFLOW", &nats.ConsumerConfig{
		Durable:       "Tracing",
		Description:   "Sequential Trace Consumer",
		DeliverPolicy: nats.DeliverAllPolicy,
		FilterSubject: subj.NS(messages.WorkflowStateAll, "*"),
		AckPolicy:     nats.AckExplicitPolicy,
		MaxAckPending: 1,
	}); err != nil {
		panic(err)
	}

	ctx := context.Background()
	svr := server.New(ctx, js, res, exp)
	if err := svr.Listen(); err != nil {
		panic(err)
	}
	time.Sleep(100 * time.Hour)
}

// EnsureConsumer sets up a new NATS consumer if one does not already exist.
func EnsureConsumer(js nats.JetStreamContext, streamName string, consumerConfig *nats.ConsumerConfig) error {
	if _, err := js.ConsumerInfo(streamName, consumerConfig.Durable); err == nats.ErrConsumerNotFound {
		if _, err := js.AddConsumer(streamName, consumerConfig); err != nil {
			panic(err)
		}
	} else if err != nil {
		return err
	}
	return nil
}
