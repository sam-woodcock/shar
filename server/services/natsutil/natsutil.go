package natsutil

import (
	"crypto/rand"
	"github.com/nats-io/nats.go"
	"google.golang.org/protobuf/proto"
	"math/big"
	"strings"
	"time"
)

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
			maxJitter := &big.Int{}
			maxJitter.SetInt64(5000)
			if strings.Index(err.Error(), "wrong last sequence") > -1 {
				dur, err := rand.Int(rand.Reader, maxJitter) // Jitter
				if err != nil {
					panic("could not read random")
				}
				time.Sleep(time.Duration(dur.Int64()))
				continue
			}
			return err
		}
		break
	}
	return nil
}
