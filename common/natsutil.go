package common

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"github.com/nats-io/nats.go"
	"gitlab.com/shar-workflow/shar/common/workflow"
	errors2 "gitlab.com/shar-workflow/shar/server/errors"
	"go.uber.org/zap"
	"google.golang.org/protobuf/proto"
	"math/big"
	"strconv"
	"strings"
	"time"
)

type NatsConn interface {
	JetStream(opts ...nats.JSOpt) (nats.JetStreamContext, error)
	QueueSubscribe(subj string, queue string, cb nats.MsgHandler) (*nats.Subscription, error)
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
			if strings.Contains(err.Error(), "wrong last sequence") {
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
		return nil, fmt.Errorf("failed to load value from KV: %w", err)
	}
}

func SaveObj(_ context.Context, wf nats.KeyValue, k string, v proto.Message) error {
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

func UpdateObj[T proto.Message](ctx context.Context, wf nats.KeyValue, k string, msg T, updateFn func(v T) (T, error)) error {
	if oldk, err := wf.Get(k); err == nats.ErrKeyNotFound || (err == nil && oldk.Value() == nil) {
		if err := SaveObj(ctx, wf, k, msg); err != nil {
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

func Process(ctx context.Context, js nats.JetStreamContext, log *zap.Logger, traceName string, closer chan struct{}, subject string, durable string, concurrency int, fn func(ctx context.Context, msg *nats.Msg) (bool, error)) {
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
}

func EnsureConsumer(js nats.JetStreamContext, streamName string, consumerConfig *nats.ConsumerConfig) error {
	if _, err := js.ConsumerInfo(streamName, consumerConfig.Durable); err == nats.ErrConsumerNotFound {
		if _, err := js.AddConsumer(streamName, consumerConfig); err != nil {
			panic(err)
		}
	} else if err != nil {
		return err
	}
	return nil
}

func EnsureStream(js nats.JetStreamContext, streamConfig *nats.StreamConfig) error {
	if _, err := js.StreamInfo(streamConfig.Name); err == nats.ErrStreamNotFound {
		if _, err := js.AddStream(streamConfig); err != nil {
			return err
		}
	} else if err != nil {
		return err
	}
	return nil
}
