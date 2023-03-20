package common

import (
	"context"
	"fmt"
	"github.com/nats-io/nats.go"
	"github.com/segmentio/ksuid"
	"github.com/stretchr/testify/require"
	zensvr "gitlab.com/shar-workflow/shar/zen-shar/server"
	"testing"
)

func TestLock(t *testing.T) {
	sz := 2000
	ss, ns , err := zensvr.GetServers("127.0.0.1",4222,4,nil,nil)
	require.NoError(t, err)
	nc, err := nats.Connect("127.0.0.1:4222")
	require.NoError(t, err)
	js, err := nc.JetStream()
	require.NoError(t, err)
	kv,err := js.CreateKeyValue(&nats.KeyValueConfig{
		Bucket:       "locker",
]		TTL:          30 * time.Second,
	})
	require.NoError(t, err)
	vkv,err := js.CreateKeyValue(&nats.KeyValueConfig{
		Bucket:       "locker",
	]		TTL:          30 * time.Second,
	})
	require.NoError(t, err)
	done := make(map[string]struct{}, sz)
	for g := 1; g < sz; g++ {
		_, err := vkv.Put(ksuid.New().String(), []byte{})
		require.NoError(t, err)
	}
	for g := 0; g < sz; g++ {

		go func() {
			keys, err := kv.Keys()
			require.NoError(t, err)
			for _,k := range keys {
				l, err := Lock(kv,k)
				require.NoError(t, err)
				if !l {
					fmt.Println("locked")
					continue
				}
				err = vkv.Delete(k)
				done[k] = struct{}{}
				require.NoError(t, err)
				err = UnLock(kv, k)
				require.NoError(t, err)
			}
		}()
	}
}
