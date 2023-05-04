package data

import (
	"context"
	"fmt"
	"github.com/nats-io/nats.go"
	"gitlab.com/shar-workflow/shar/internal/client/api"
	"gitlab.com/shar-workflow/shar/model"
	"gitlab.com/shar-workflow/shar/server/messages"
)

// Client implements a SHAR data client capable of retrieving raw data about workflow history
type Client struct {
	con         *nats.Conn
	storageType nats.StorageType
	ns          string
	concurrency int
}

// Option represents a configuration changer for the client.
type Option interface {
	configure(client *Client)
}

// New creates a new SHAR data client instance
func New(option ...Option) *Client {
	client := &Client{
		storageType: nats.FileStorage,
		ns:          "default",
		concurrency: 10,
	}
	for _, i := range option {
		i.configure(client)
	}
	return client
}

// Dial instructs the client to connect to a NATS server.
func (c *Client) Dial(natsURL string, opts ...nats.Option) error {
	n, err := nats.Connect(natsURL, opts...)
	if err != nil {
		return fmt.Errorf("data client dial: %w", err)
	}

	c.con = n
	return nil
}

// SpoolWorkflowEvents provides an interface to a datawarehousing application to recieve a stream of workflow events through polling.
func (c *Client) SpoolWorkflowEvents(ctx context.Context) (*model.SpoolWorkflowEventsResponse, error) {
	req := &model.SpoolWorkflowEventsRequest{}
	res := &model.SpoolWorkflowEventsResponse{}
	if err := api.Call(ctx, c.con, messages.APISpoolWorkflowEvents, req, res); err != nil {
		return nil, fmt.Errorf("spooling: %w", err)
	}
	return res, nil
}
