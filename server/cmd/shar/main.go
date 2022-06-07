package main

import (
	"github.com/crystal-construct/shar/server/config"
	"github.com/crystal-construct/shar/server/grpc"
	"github.com/crystal-construct/shar/telemetry"
	traceSdk "go.opentelemetry.io/otel/sdk/trace"
	"log"
)

const serviceName = "shar"

func main() {
	cfg, err := config.GetEnvironment()
	if err != nil {
		log.Fatal(err)
	}

	var exp traceSdk.SpanExporter
	if cfg.JaegerURL != "" {
		exp = telemetry.GetJaegerExporterOrNoop("")
		if _, err := telemetry.RegisterOpenTelemetry(exp, serviceName); err != nil {
			panic(err)
		}
	}

	svr := grpc.NewSharServer(exp)
	svr.Listen(cfg.NatsURL, cfg.Port)
}
