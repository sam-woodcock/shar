package server

//go:generate mockery --name Exporter --outpkg intTest --filename exporter_mock_test.go --output ../../integration/shar --structname MockTelemetry

import (
	"context"
	tracesdk "go.opentelemetry.io/otel/sdk/trace"
)

// Exporter respresents an interface to the span exporter
type Exporter interface {
	ExportSpans(ctx context.Context, spans []tracesdk.ReadOnlySpan) error
}
