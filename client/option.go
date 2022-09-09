package client

import "github.com/nats-io/nats.go"

func WithEphemeralStorage() ephemeralStorage {
	return ephemeralStorage{}
}

type ephemeralStorage struct{}

func (o ephemeralStorage) configure(client *Client) {
	client.storageType = nats.MemoryStorage
}
