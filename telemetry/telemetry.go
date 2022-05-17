package telemetry

import (
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/exporters/jaeger"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/resource"
	tracesdk "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	semconv "go.opentelemetry.io/otel/semconv/v1.10.0"
	"log"
)

func GetJaegerExporterOrNoop(url string) tracesdk.SpanExporter {
	if len(url) > 0 {
		// Create the Jaeger exporter
		if exp, err := jaeger.New(jaeger.WithCollectorEndpoint(jaeger.WithEndpoint(url))); err != nil {
			log.Printf("ERROR: failed to create jaeger exporter from url(%v): %v", url, err)
			return tracetest.NewNoopExporter()
		} else {
			return exp
		}
	} else {
		return tracetest.NewNoopExporter()
	}
}

func RegisterOpenTelemetry(exp tracesdk.SpanExporter, serviceName string) (*tracesdk.TracerProvider, error) {
	tp := tracesdk.NewTracerProvider(
		// Always be sure to batch in production.
		tracesdk.WithBatcher(exp),
		// Record information about this application in a Resource.
		tracesdk.WithResource(resource.NewWithAttributes(
			semconv.SchemaURL,
			semconv.ServiceNameKey.String(serviceName),
			attribute.String("environment", "prod"),
			attribute.Int64("ID", 65537),
		)),
	)
	otel.SetTracerProvider(tp)

	otel.SetTextMapPropagator(
		propagation.NewCompositeTextMapPropagator(
			propagation.TraceContext{},
			propagation.Baggage{},
		),
	)
	return tp, nil
}
