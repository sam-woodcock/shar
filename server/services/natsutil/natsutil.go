package natsutil

import (
	"github.com/nats-io/nats.go"
	"google.golang.org/protobuf/proto"
	"math/rand"
	"strings"
	"time"
)

func init() {
	rand.Seed(time.Now().UnixNano())
}

func UpdateKV(wf nats.KeyValue, k string, msg proto.Message, updateFn func(v []byte, msg proto.Message) ([]byte, error)) error {
	for {
		entry, err := wf.Get(k)
		if err != nil {
			return err
		}
		rev := entry.Revision()
		uv, err := updateFn(entry.Value(), msg)
		if err != nil {
			return err
		}
		_, err = wf.Update(k, uv, rev)
		// TODO: Horrible workaround for the fact that this is not a typed error
		if err != nil {
			if strings.Index(err.Error(), "wrong last sequence") > -1 {
				dur := int64(rand.Intn(50)) * 1000 // Jitter
				time.Sleep(time.Duration(dur))
				continue
			}
			return err
		}
		break
	}
	return nil
}
