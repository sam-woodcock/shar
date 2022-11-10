package config

import (
	"github.com/caarlos0/env/v6"
)

// Settings is the settings provider for a SHAR server.
type Settings struct {
	Port          int    `env:"SHAR_PORT" envDefault:"50000"`
	NatsURL       string `env:"NATS_URL" envDefault:"nats://127.0.0.1:4222"`
	LogLevel      string `env:"SHAR_LOG_LEVEL" envDefault:"debug"`
	PanicRecovery bool   `env:"SHAR_PANIC_RECOVERY" envDefault:"true"`
}

// GetEnvironment pulls the active settings into a settings struct.
func GetEnvironment() (*Settings, error) {
	cfg := &Settings{}
	if err := env.Parse(cfg); err != nil {
		return nil, err
	}
	return cfg, nil
}
