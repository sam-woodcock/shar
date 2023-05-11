package client

import (
	"crypto/rand"
	"fmt"
	version2 "github.com/hashicorp/go-version"
	"github.com/stretchr/testify/require"
	"gitlab.com/shar-workflow/shar/common/version"
	zensvr "gitlab.com/shar-workflow/shar/zen-shar/server"
	"math/big"
	"testing"
	"time"
)

func TestHigherClientVersion(t *testing.T) {
	natsHost := "127.0.0.1"
	v, e := rand.Int(rand.Reader, big.NewInt(500))
	require.NoError(t, e)
	natsPort := 4459 + int(v.Int64())
	natsURL := fmt.Sprintf("nats://%s:%v", natsHost, natsPort)
	version.Version = "1.0.503"
	ss, ns, err := zensvr.GetServers(natsHost, natsPort, 4, nil, nil)
	require.NoError(t, err)
	go ns.Start()
	ns.ReadyForConnections(5 * time.Second)
	go ss.Listen(natsURL, 5050)
	forcedVersion, err := version2.NewVersion("v1.0.800")
	require.NoError(t, err)
	cl := New(forceVersion{ver: forcedVersion})
	err = cl.Dial(natsURL)
	require.Error(t, err)
}

type forceVersion struct {
	ver *version2.Version
}

func (o forceVersion) configure(client *Client) {
	client.version = o.ver
	client.ExpectedCompatibleServerVersion = o.ver
}
