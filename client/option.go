package client

import "github.com/nats-io/nats.go"

func WithEphemeralStorage() EphemeralStorage {
	return EphemeralStorage{}
}

type EphemeralStorage struct{}

func (o EphemeralStorage) configure(client *Client) {
	client.storageType = nats.MemoryStorage
}
