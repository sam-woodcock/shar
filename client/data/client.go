package data

import (
	"context"
	"fmt"
	"github.com/hashicorp/go-version"
	"github.com/nats-io/nats.go"
	"gitlab.com/shar-workflow/shar/common/setup/upgrader"
	version2 "gitlab.com/shar-workflow/shar/common/version"
	"gitlab.com/shar-workflow/shar/internal/client/api"
	"gitlab.com/shar-workflow/shar/model"
	"gitlab.com/shar-workflow/shar/server/messages"
)

// Client implements a SHAR data client capable of retrieving raw data about workflow history
type Client struct {
	con                             *nats.Conn
	storageType                     nats.StorageType
	ns                              string
	concurrency                     int
	ExpectedServerVersion           *version.Version
	ExpectedCompatibleServerVersion *version.Version
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
func (c *Client) Dial(ctx context.Context, natsURL string, opts ...nats.Option) error {
	n, err := nats.Connect(natsURL, opts...)
	if err != nil {
		return fmt.Errorf("data client dial: %w", err)
	}
	c.con = n

	_, err = c.GetServerVersion(ctx)
	if err != nil {
		return fmt.Errorf("server version: %w", err)
	}
	return nil
}

// GetServerVersion returns the current server version.
func (c *Client) GetServerVersion(ctx context.Context) (*version.Version, error) {
	req := &model.GetVersionInfoRequest{
		ClientVersion: version2.Version,
	}
	res := &model.GetVersionInfoResponse{}
	if err := api.Call(ctx, c.con, messages.APIGetVersionInfo, c.ExpectedCompatibleServerVersion, req, res); err != nil {
		return nil, fmt.Errorf("get version info: %w", err)
	}

	sv, err := version.NewVersion(res.ServerVersion)
	if err != nil {
		return nil, fmt.Errorf("get server version info: %w", err)
	}
	cv, err := version.NewVersion(res.MinCompatibleVersion)
	if err != nil {
		return nil, fmt.Errorf("get server version info: %w", err)
	}
	c.ExpectedServerVersion = sv
	c.ExpectedCompatibleServerVersion = cv

	if !res.Connect {
		return sv, fmt.Errorf("incompatible client version: client must be " + cv.String())
	}

	ok, cv2 := upgrader.IsCompatible(sv)
	if !ok {
		return sv, fmt.Errorf("incompatible server version: " + sv.String() + " server must be " + cv2.String())
	}
	return sv, nil
}

// SpoolWorkflowEvents provides an interface to a datawarehousing application to recieve a stream of workflow events through polling.
func (c *Client) SpoolWorkflowEvents(ctx context.Context) (*model.SpoolWorkflowEventsResponse, error) {
	req := &model.SpoolWorkflowEventsRequest{}
	res := &model.SpoolWorkflowEventsResponse{}
	if err := api.Call(ctx, c.con, messages.APISpoolWorkflowEvents, c.ExpectedCompatibleServerVersion, req, res); err != nil {
		return nil, fmt.Errorf("spooling: %w", err)
	}
	return res, nil
}
