package model

import "github.com/nats-io/nats.go"

type natsconfig struct {
	Streams   []nats.StreamConfig
	Consumers []nats.ConsumerConfig
	KeyValues []nats.KeyValueConfig
}
