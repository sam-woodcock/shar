package services

import (
	"context"
	"fmt"
	"github.com/nats-io/nats.go"
	"gitlab.com/shar-workflow/shar/common"
	"gitlab.com/shar-workflow/shar/model"
	"go.uber.org/zap"
)

type NatsClientProvider struct {
	wf         nats.KeyValue
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

func (s *NatsClientProvider) GetJob(_ context.Context, id string) (*model.WorkflowState, error) {
	job := &model.WorkflowState{}
	if err := common.LoadObj(s.job, id, job); err != nil {
		return nil, err
	}
	return job, nil
}
