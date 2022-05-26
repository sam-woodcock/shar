package services

import (
	"context"
	"fmt"
	"github.com/crystal-construct/shar/model"
	"github.com/crystal-construct/shar/server/errors"
	"github.com/nats-io/nats.go"
	"github.com/segmentio/ksuid"
	"github.com/uptrace/opentelemetry-go-extra/otelzap"
	"go.uber.org/zap"
	"google.golang.org/protobuf/proto"
)

type NatsKVStore struct {
	wf         nats.KeyValue
	latestWf   nats.KeyValue
	wfInstance nats.KeyValue
	log        *otelzap.Logger
	job        nats.KeyValue
	conn       *nats.Conn
	js         nats.JetStreamContext
}

func NewNatsKVStore(log *otelzap.Logger, conn *nats.Conn, storageType nats.StorageType) (*NatsKVStore, error) {
	js, err := conn.JetStream()
	if err != nil {
		return nil, err
	}
	if err := ensureBuckets(js, storageType, []string{"WORKFLOW_INSTANCE", "WORKFLOW_DEF", "WORKFLOW_LATEST", "WORKFLOW_JOB"}); err != nil {
		return nil, err
	}
	ms := &NatsKVStore{
		conn: conn,
		js:   js,
		log:  log,
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

	if kv, err := js.KeyValue("WORKFLOW_LATEST"); err != nil {
		return nil, err
	} else {
		ms.latestWf = kv
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
				return fmt.Errorf("failed to ensure buckets: %w", err)
			}
		} else if err != nil {
			return fmt.Errorf("failed to obtain bucket: %w", err)
		}
	}
	return nil
}

func (s *NatsKVStore) StoreWorkflow(ctx context.Context, process *model.Process) (string, error) {
	wfId := ksuid.New().String()
	if err := SaveObj(s.wf, wfId, process); err != nil {
		return "", err
	}
	if err := Save(s.latestWf, process.Name, []byte(wfId)); err != nil {
		return "", err
	}
	return wfId, nil
}

func Save(wf nats.KeyValue, k string, v []byte) error {
	_, err := wf.Put(k, v)
	return err
}

func Load(wf nats.KeyValue, k string) ([]byte, error) {
	if b, err := wf.Get(k); err == nil {
		return b.Value(), nil
	} else {
		return nil, err
	}
}

func SaveObj(wf nats.KeyValue, k string, v proto.Message) error {
	if b, err := proto.Marshal(v); err == nil {
		return Save(wf, k, b)
	} else {
		return err
	}
}

func LoadObj(wf nats.KeyValue, k string, v proto.Message) error {
	if kv, err := Load(wf, k); err == nil {
		return proto.Unmarshal(kv, v)
	} else {
		return err
	}
}

func (s *NatsKVStore) GetWorkflow(ctx context.Context, workflowId string) (*model.Process, error) {
	process := &model.Process{}
	if err := LoadObj(s.wf, workflowId, process); err == nats.ErrKeyNotFound {
		return nil, errors.ErrWorkflowNotFound
	} else if err != nil {
		return nil, err
	}
	return process, nil
}

func (s *NatsKVStore) CreateWorkflowInstance(ctx context.Context, wfInstance *model.WorkflowInstance) (*model.WorkflowInstance, error) {
	wfiId := ksuid.New().String()
	wfInstance.WorkflowInstanceId = wfiId
	if err := SaveObj(s.wfInstance, wfiId, wfInstance); err != nil {
		return nil, err
	}
	return wfInstance, nil
}

func (s *NatsKVStore) GetWorkflowInstance(ctx context.Context, workflowInstanceId string) (*model.WorkflowInstance, error) {
	wfi := &model.WorkflowInstance{}
	if err := LoadObj(s.wfInstance, workflowInstanceId, wfi); err == nats.ErrKeyNotFound {
		return nil, errors.ErrWorkflowNotFound
	} else if err != nil {
		return nil, err
	}
	return wfi, nil
}

func (s *NatsKVStore) DestroyWorkflowInstance(ctx context.Context, workflowInstanceId string) error {
	return s.wfInstance.Delete(workflowInstanceId)
}

func (s *NatsKVStore) GetLatestVersion(ctx context.Context, workflowName string) (string, error) {
	if v, err := Load(s.latestWf, workflowName); err == nats.ErrKeyNotFound {
		return "", errors.ErrWorkflowNotFound
	} else if err != nil {
		return "", err
	} else {
		return string(v), nil
	}
}

func (s *NatsKVStore) CreateJob(ctx context.Context, job *model.Job) (string, error) {
	jobId := ksuid.New()
	job.Id = jobId.String()
	if err := SaveObj(s.job, jobId.String(), job); err != nil {
		return "", err
	}
	return jobId.String(), nil
}

func (s *NatsKVStore) GetJob(ctx context.Context, id string) (*model.Job, error) {
	job := &model.Job{}
	if err := LoadObj(s.job, id, job); err != nil {
		return nil, err
	}
	return job, nil
}

func (s *NatsKVStore) ListWorkflowInstance(workflowId string) (chan *model.WorkflowInstance, chan error) {

	wch := make(chan *model.WorkflowInstance, 100)
	errs := make(chan error, 1)
	keys, err := s.wfInstance.Keys()
	if err != nil {
		s.log.Error("error obtaining keys", zap.Error(err))
		return nil, errs
	}
	go func(keys []string) {
		for _, k := range keys {
			v := &model.WorkflowInstance{}
			err := LoadObj(s.wfInstance, k, v)
			if err != nil && err != nats.ErrKeyNotFound {
				errs <- err
				s.log.Error("error loading object", zap.Error(err))
				close(errs)
				return
			}
			if workflowId == "" || v.WorkflowId == workflowId {
				wch <- v
			}
		}
		close(wch)
	}(keys)
	return wch, errs
}
