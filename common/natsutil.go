package common

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"github.com/nats-io/nats.go"
	"gitlab.com/shar-workflow/shar/common/setup"
	"gitlab.com/shar-workflow/shar/common/workflow"
	errors2 "gitlab.com/shar-workflow/shar/server/errors"
	"go.uber.org/zap"
	"google.golang.org/protobuf/proto"
	"math/big"
	"strconv"
	"strings"
	"time"
)

// NatsConn is the trimmad down NATS Connection interface that only emcompasses the methods used by SHAR
type NatsConn interface {
	JetStream(opts ...nats.JSOpt) (nats.JetStreamContext, error)
	QueueSubscribe(subj string, queue string, cb nats.MsgHandler) (*nats.Subscription, error)
}

func updateKV(wf nats.KeyValue, k string, msg proto.Message, updateFn func(v []byte, msg proto.Message) ([]byte, error)) error {
	for {
		entry, err := wf.Get(k)
		if err != nil {
			return fmt.Errorf("failed to get value to update: %w", err)
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
			if strings.Contains(err.Error(), "wrong last sequence") {
				dur, err := rand.Int(rand.Reader, maxJitter) // Jitter
				if err != nil {
					panic("could not read random")
				}
				time.Sleep(time.Duration(dur.Int64()))
				continue
			}
			return fmt.Errorf("failed to update kv: %w", err)
		}
		break
	}
	return nil
}

// Save saves a value to a key value store
func Save(wf nats.KeyValue, k string, v []byte) error {
	if _, err := wf.Put(k, v); err != nil {
		return fmt.Errorf("failed to save kv: %w", err)
	}
	return nil
}

// Load loads a value from a key value store
func Load(wf nats.KeyValue, k string) ([]byte, error) {
	b, err := wf.Get(k)
	if err == nil {
		return b.Value(), nil
	}
	return nil, fmt.Errorf("failed to load value from KV: %w", err)
}

// SaveObj save an protobuf message to a key value store
func SaveObj(_ context.Context, wf nats.KeyValue, k string, v proto.Message) error {
	b, err := proto.Marshal(v)
	if err == nil {
		return Save(wf, k, b)
	}
	return fmt.Errorf("failed to save object into KV: %w", err)
}

// LoadObj loads a protobuf message from a key value store
func LoadObj(wf nats.KeyValue, k string, v proto.Message) error {
	kv, err := Load(wf, k)
	if err != nil {
		return fmt.Errorf("failed to load object from KV %s(%s): %w", wf.Bucket(), k, err)
	}
	if err := proto.Unmarshal(kv, v); err != nil {
		return fmt.Errorf("failed to unmarshal in LoadObj: %w", err)
	}
	return nil
}

// UpdateObj saves an protobuf message to a key value store after using updateFN to update the message.
func UpdateObj[T proto.Message](ctx context.Context, wf nats.KeyValue, k string, msg T, updateFn func(v T) (T, error)) error {
	if oldk, err := wf.Get(k); err == nats.ErrKeyNotFound || (err == nil && oldk.Value() == nil) {
		if err := SaveObj(ctx, wf, k, msg); err != nil {
			return err
		}
	}
	return updateKV(wf, k, msg, func(bv []byte, msg proto.Message) ([]byte, error) {
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

// Delete deletes an item from a key value store.
func Delete(kv nats.KeyValue, key string) error {
	if err := kv.Delete(key); err != nil {
		return fmt.Errorf("failed to delete key: %w", err)
	}
	return nil
}

// EnsureBuckets ensures that a list of key value stores exist
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

// Process processes messages from a nats consumer and executes a function against each one.
func Process(ctx context.Context, js nats.JetStreamContext, log *zap.Logger, traceName string, closer chan struct{}, subject string, durable string, concurrency int, fn func(ctx context.Context, msg *nats.Msg) (bool, error)) error {
	if _, ok := setup.ConsumerDurableNames[durable]; !strings.HasPrefix(durable, "ServiceTask_") && !ok {
		return fmt.Errorf("durable consumer '%s' is not explicity configured", durable)
	}
	for i := 0; i < concurrency; i++ {
		go func() {
			sub, err := js.PullSubscribe(subject, durable)
			if err != nil {
				log.Error("process pull subscribe error", zap.Error(err), zap.String("subject", subject), zap.String("durable", durable))
				return
			}
			for {
				select {
				case <-closer:
					return
				default:
				}
				reqCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
				msg, err := sub.Fetch(1, nats.Context(reqCtx))
				if err != nil {
					if err == context.DeadlineExceeded {
						cancel()
						continue
					}
					// Log Error
					log.Error("message fetch error", zap.Error(err))
					cancel()
					continue
				}
				m := msg[0]
				//				log.Debug("Process:"+traceName, zap.String("subject", msg[0].Subject))
				cancel()
				if embargo := m.Header.Get("embargo"); embargo != "" && embargo != "0" {
					e, err := strconv.Atoi(embargo)
					if err != nil {
						log.Error("bad embargo value", zap.Error(err))
						continue
					}
					offset := time.Duration(int64(e) - time.Now().UnixNano())
					if offset > 0 {
						if err != m.NakWithDelay(offset) {
							log.Warn("failed to nak with delay")
						}
						continue
					}
				}
				executeCtx := context.Background()
				ack, err := fn(executeCtx, msg[0])
				if err != nil {
					if errors2.IsWorkflowFatal(err) {
						log.Error("workflow fatal error occured processing function", zap.Error(err))
						ack = true
					} else {
						wfe := &workflow.Error{}
						if !errors.As(err, wfe) {
							log.Error("processing error", zap.Error(err), zap.String("name", traceName))
						}
					}
				}
				if ack {
					if err := msg[0].Ack(); err != nil {
						log.Error("processing failed to ack", zap.Error(err))
					}
				} else {
					if err := msg[0].Nak(); err != nil {
						log.Error("processing failed to nak", zap.Error(err))
					}
				}
			}
		}()
	}
	return nil
}
