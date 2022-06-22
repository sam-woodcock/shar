package services

import (
	"context"
	"fmt"
	"github.com/crystal-construct/shar/model"
	"github.com/nats-io/nats.go"
	"go.uber.org/zap"
	"google.golang.org/protobuf/proto"
)

type NatsClientProvider struct {
	wf         nats.KeyValue
	latestWf   nats.KeyValue
	wfInstance nats.KeyValue
	log        *zap.Logger
	job        nats.KeyValue
	js         nats.JetStreamContext
}

func NewNatsClientProvider(log *zap.Logger, js nats.JetStreamContext, storageType nats.StorageType) (*NatsClientProvider, error) {
	if err := ensureBuckets(js, storageType, []string{"WORKFLOW_INSTANCE", "WORKFLOW_DEF", "WORKFLOW_JOB"}); err != nil {
		return nil, err
	}
	ms := &NatsClientProvider{
		js:  js,
		log: log,
	}
	if kv, err := js.KeyValue("WORKFLOW_INSTANCE"); err != nil {
		return nil, err
	} else {
		ms.wfInstance = kv
	}

	if kv, err := js.KeyValue("WORKFLOW_DEF"); err != nil {
		return nil, err
	} else {
		ms.wf = kv
	}

	if kv, err := js.KeyValue("WORKFLOW_JOB"); err != nil {
		return nil, err
	} else {
		ms.job = kv
	}
	return ms, nil
}
func ensureBuckets(js nats.JetStreamContext, storageType nats.StorageType, names []string) error {
	for _, i := range names {
		if _, err := js.KeyValue(i); err == nats.ErrBucketNotFound {
			if _, err := js.CreateKeyValue(&nats.KeyValueConfig{Bucket: i, Storage: storageType}); err != nil {
				return fmt.Errorf("failed to create KV: %w", err)
			}
		} else if err != nil {
			return fmt.Errorf("failed to ensure buckets: %w", err)
		}
	}
	return nil
}

func Save(wf nats.KeyValue, k string, v []byte) error {
	_, err := wf.Put(k, v)
	if err != nil {
		return fmt.Errorf("failed to save value: %w", err)
	}
	return nil
}

func Load(wf nats.KeyValue, k string) ([]byte, error) {
	if b, err := wf.Get(k); err == nil {
		return b.Value(), nil
	} else {
		return nil, fmt.Errorf("failed to load value: %w", err)
	}
}

func SaveObj(wf nats.KeyValue, k string, v proto.Message) error {
	if b, err := proto.Marshal(v); err == nil {
		return Save(wf, k, b)
	} else {
		return fmt.Errorf("failed to save object: %w", err)
	}
}

func LoadObj(wf nats.KeyValue, k string, v proto.Message) error {
	if kv, err := Load(wf, k); err == nil {
		return proto.Unmarshal(kv, v)
	} else {
		return fmt.Errorf("failed to load object: %w", err)
	}
}

func (s *NatsClientProvider) GetJob(ctx context.Context, id string) (*model.WorkflowState, error) {
	job := &model.WorkflowState{}
	if err := LoadObj(s.job, id, job); err != nil {
		return nil, err
	}
	return job, nil
}
