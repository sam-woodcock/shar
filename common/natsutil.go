package common

import (
	"context"
	"crypto/rand"
	"fmt"
	"github.com/nats-io/nats.go"
	"google.golang.org/protobuf/proto"
	"math/big"
	"strings"
	"time"
)

type NatsConn interface {
	JetStream(opts ...nats.JSOpt) (nats.JetStreamContext, error)
	Subscribe(subject string, fn nats.MsgHandler) (*nats.Subscription, error)
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

func Save(wf nats.KeyValue, k string, v []byte) error {
	_, err := wf.Put(k, v)
	return err
}

func Load(wf nats.KeyValue, k string) ([]byte, error) {
	if b, err := wf.Get(k); err == nil {
		return b.Value(), nil
	} else {
		return nil, fmt.Errorf("failed to load value into KV: %w", err)
	}
}

func SaveObj(ctx context.Context, wf nats.KeyValue, k string, v proto.Message) error {
	if b, err := proto.Marshal(v); err == nil {
		return Save(wf, k, b)
	} else {
		return fmt.Errorf("failed to save object into KV: %w", err)
	}
}

func LoadObj(wf nats.KeyValue, k string, v proto.Message) error {
	if kv, err := Load(wf, k); err == nil {
		return proto.Unmarshal(kv, v)
	} else {
		return fmt.Errorf("failed to load object from KV: %w", err)
	}
}

func UpdateObj[T proto.Message](wf nats.KeyValue, k string, msg T, updateFn func(v T) (T, error)) error {
	if oldk, err := wf.Get(k); err == nats.ErrKeyNotFound || (err == nil && oldk.Value() == nil) {
		if err := SaveObj(nil, wf, k, msg); err != nil {
			return err
		}
	}
	return UpdateKV(wf, k, msg, func(bv []byte, msg proto.Message) ([]byte, error) {
		if err := proto.Unmarshal(bv, msg); err != nil {
			return nil, fmt.Errorf("failed to unmarshal proto for KV update: %w", err)
		}
		uv, err := updateFn(msg.(T))
		if err != nil {
			return nil, fmt.Errorf("failed during update function: %w", err)
		}
		b, err := proto.Marshal(uv)
		if err != nil {
			return nil, fmt.Errorf("failed to marshal updated proto: %w", err)
		}
		return b, nil
	})
}

func EnsureBuckets(js nats.JetStreamContext, storageType nats.StorageType, names []string) error {
	for _, i := range names {
		if _, err := js.KeyValue(i); err == nats.ErrBucketNotFound {
			if _, err := js.CreateKeyValue(&nats.KeyValueConfig{Bucket: i, Storage: storageType}); err != nil {
				return fmt.Errorf("failed to ensure buckets: %w", err)
			}
		} else if err != nil {
			return fmt.Errorf("failed to obtain bucket: %w", err)
		}
	}
	return nil
}
