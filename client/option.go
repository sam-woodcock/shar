package client

import "github.com/nats-io/nats.go"

// WithEphemeralStorage specifies a client store the result of all operations in memory.
func WithEphemeralStorage() ephemeralStorage { //nolint
	return ephemeralStorage{}
}

type ephemeralStorage struct{}

func (o ephemeralStorage) configure(client *Client) {
	client.storageType = nats.MemoryStorage
}
