package services

import (
	"context"
	"fmt"
	"github.com/crystal-construct/shar/model"
	"github.com/crystal-construct/shar/server/services/natsutil"
	"github.com/nats-io/nats.go"
	"go.uber.org/zap"
	"google.golang.org/grpc/codes"
	"google.golang.org/protobuf/proto"
)

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

func SaveObj(wf nats.KeyValue, k string, v proto.Message) error {
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

func UpdateObj(wf nats.KeyValue, k string, msg proto.Message, updateFn func(v proto.Message) (proto.Message, error)) error {
	if oldk, err := wf.Get(k); err == nats.ErrKeyNotFound || (err == nil && oldk.Value() == nil) {
		if err := SaveObj(wf, k, &model.WorkflowVersions{Version: []*model.WorkflowVersion{}}); err != nil {
			return err
		}
	}
	var p = &model.WorkflowVersions{}
	err := LoadObj(wf, k, p)
	if err != nil {
		panic(err)
	}
	return natsutil.UpdateKV(wf, k, msg, func(bv []byte, msg proto.Message) ([]byte, error) {
		if err := proto.Unmarshal(bv, msg); err != nil {
			return nil, fmt.Errorf("failed to unmarshal proto for KV update: %w", err)
		}
		uv, err := updateFn(msg)
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
func ensureBuckets(js nats.JetStreamContext, storageType nats.StorageType, names []string) error {
	for _, i := range names {
		if _, err := js.KeyValue(i); err == nats.ErrBucketNotFound {
			if _, err := js.CreateKeyValue(&nats.KeyValueConfig{Bucket: i}); err != nil {
				return fmt.Errorf("failed to ensure buckets: %w", err)
			}
		} else if err != nil {
			return fmt.Errorf("failed to obtain bucket: %w", err)
		}
	}
	return nil
}

func stringInSlice(a string, list []string) bool {
	for _, b := range list {
		if b == a {
			return true
		}
	}
	return false
}

func Listen[T proto.Message, U proto.Message](con *nats.Conn, log *zap.Logger, subject string, req T, fn func(ctx context.Context, req T) (U, error)) (*nats.Subscription, error) {
	sub, err := con.Subscribe(subject, func(msg *nats.Msg) {
		ctx := context.Background()
		if err := callApi(ctx, req, msg, fn); err != nil {
			log.Error("registering listener for "+subject+" failed", zap.Error(err))
		}
	})
	if err != nil {
		return nil, fmt.Errorf("failed to subscribe to %s: %w", subject, err)
	}
	return sub, nil
}

func callApi[T proto.Message, U proto.Message](ctx context.Context, container T, msg *nats.Msg, fn func(ctx context.Context, req T) (U, error)) error {
	defer recoverAPIpanic(msg)
	if err := proto.Unmarshal(msg.Data, container); err != nil {
		errorResponse(msg, codes.Internal, err.Error())
		return err
	}
	resMsg, err := fn(ctx, container)
	if err != nil {
		errorResponse(msg, codes.Internal, err.Error())
		return err
	}
	res, err := proto.Marshal(resMsg)
	if err != nil {
		errorResponse(msg, codes.Internal, err.Error())
		return err
	}
	if err := msg.Respond(res); err != nil {
		errorResponse(msg, codes.Internal, err.Error())
		return err
	}
	return nil
}

func recoverAPIpanic(msg *nats.Msg) {
	if r := recover(); r != nil {
		errorResponse(msg, codes.Internal, r)
		fmt.Println("recovered from ", r)
	}
}

func errorResponse(m *nats.Msg, code codes.Code, msg any) {
	if err := m.Respond(apiError(codes.Internal, msg)); err != nil {
		fmt.Println("failed to send error response: " + string(apiError(codes.Internal, msg)))
	}
}

func apiError(code codes.Code, msg any) []byte {
	return []byte(fmt.Sprintf("ERR_%d|%+v", code, msg))
}
