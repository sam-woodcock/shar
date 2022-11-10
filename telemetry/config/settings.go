package config

import (
	"github.com/caarlos0/env/v6"
)

// Settings is the settings provider for telemetry.
type Settings struct {
	NatsURL   string `env:"NATS_URL" envDefault:"nats://127.0.0.1:4222"`
	JaegerURL string `env:"JAEGER_URL" envDefault:"http://localhost:14268/api/traces"`
	LogLevel  string `env:"SHAR_LOG_LEVEL" envDefault:"debug"`
}

// GetEnvironment pulls the active settings into a settings struct.
func GetEnvironment() (*Settings, error) {
	cfg := &Settings{}
	if err := env.Parse(cfg); err != nil {
		return nil, err
	}
	return cfg, nil
}
