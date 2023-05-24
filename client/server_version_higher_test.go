package client

import (
	"context"
	"crypto/rand"
	"fmt"
	version2 "github.com/hashicorp/go-version"
	"github.com/stretchr/testify/require"
	zenSvr "gitlab.com/shar-workflow/shar/zen-shar/server"
	"math/big"
	"testing"
	"time"
)

func TestHigherServerVersion(t *testing.T) {
	natsHost := "127.0.0.1"
	v, e := rand.Int(rand.Reader, big.NewInt(500))
	require.NoError(t, e)
	natsPort := 4459 + int(v.Int64())
	natsURL := fmt.Sprintf("nats://%s:%v", natsHost, natsPort)

	ss, ns, err := zenSvr.GetServers(natsHost, natsPort, 4, nil, nil)
	require.NoError(t, err)
	go ns.Start()
	ns.ReadyForConnections(5 * time.Second)
	go ss.Listen(natsURL, 5050)
	forcedVersion, err := version2.NewVersion("v1.0.100")
	require.NoError(t, err)
	cl := New(forceVersion{ver: forcedVersion})
	ctx := context.Background()
	err = cl.Dial(ctx, natsURL)
	require.Error(t, err)
}
