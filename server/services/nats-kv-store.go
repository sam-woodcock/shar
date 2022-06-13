package services

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"github.com/crystal-construct/shar/model"
	"github.com/crystal-construct/shar/server/errors"
	"github.com/crystal-construct/shar/server/services/natsutil"
	"github.com/nats-io/nats.go"
	"github.com/segmentio/ksuid"
	"go.uber.org/zap"
	"google.golang.org/protobuf/proto"
)

type NatsKVStore struct {
	wf         nats.KeyValue
	wfVersion  nats.KeyValue
	wfInstance nats.KeyValue
	wfTracking nats.KeyValue
	log        *zap.Logger
	job        nats.KeyValue
	conn       *nats.Conn
	js         nats.JetStreamContext
}

func (s *NatsKVStore) ListWorkflows() (chan *model.ListWorkflowResult, chan error) {
	res := make(chan *model.ListWorkflowResult, 100)
	errs := make(chan error, 1)
	keys, err := s.wfVersion.Keys()
	if err == nats.ErrNoKeysFound {
		keys = []string{}
	} else if err != nil {
		errs <- err
		return res, errs
	}
	go func() {
		for _, k := range keys {
			v := &model.WorkflowVersions{}
			err := LoadObj(s.wfVersion, k, v)
			if err == nats.ErrKeyNotFound {
				continue
			}
			if err != nil {
				errs <- err
			}
			res <- &model.ListWorkflowResult{
				Name:    k,
				Version: v.Version[0].Number,
			}

		}
		close(res)
	}()
	return res, errs
}

func NewNatsKVStore(log *zap.Logger, conn *nats.Conn, storageType nats.StorageType) (*NatsKVStore, error) {
	js, err := conn.JetStream()
	if err != nil {
		return nil, err
	}
	if err := ensureBuckets(js, storageType, []string{"WORKFLOW_INSTANCE", "WORKFLOW_TRACKING", "WORKFLOW_DEF", "WORKFLOW_LATEST", "WORKFLOW_JOB", "WORKFLOW_VERSION"}); err != nil {
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

	if kv, err := js.KeyValue("WORKFLOW_TRACKING"); err != nil {
		return nil, err
	} else {
		ms.wfTracking = kv
	}

	if kv, err := js.KeyValue("WORKFLOW_DEF"); err != nil {
		return nil, err
	} else {
		ms.wf = kv
	}

	if kv, err := js.KeyValue("WORKFLOW_VERSION"); err != nil {
		return nil, err
	} else {
		ms.wfVersion = kv
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
			if _, err := js.CreateKeyValue(&nats.KeyValueConfig{Bucket: i}); err != nil {
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
	b, err := json.Marshal(process)
	if err != nil {
		return "", err
	}
	h := sha256.New()
	h.Write(b)
	hash := h.Sum(nil)
	err = SaveObj(s.wf, wfId, process)
	if err != nil {
		return "", err
	}
	err = UpdateObj(s.wfVersion, process.Name, &model.WorkflowVersions{}, func(v1 proto.Message) (proto.Message, error) {
		v := v1.(*model.WorkflowVersions)
		if v.Version == nil || len(v.Version) == 0 {
			v.Version = make([]*model.WorkflowVersion, 0, 1)
		} else {
			if bytes.Equal(hash, v.Version[0].Sha256) {
				return v, nil
			}
		}
		v.Version = append([]*model.WorkflowVersion{
			{Id: wfId, Sha256: hash, Number: int32(len(v.Version)) + 1},
		}, v.Version...)
		return v, nil
	})
	if err != nil {
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
			return nil, err
		}
		uv, err := updateFn(msg)
		if err != nil {
			return nil, err
		}
		b, err := proto.Marshal(uv)
		if err != nil {
			return nil, err
		}
		return b, nil
	})
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
	if err := s.wfInstance.Delete(workflowInstanceId); err != nil {
		return err
	}
	if err := s.wfTracking.Delete(workflowInstanceId); err != nil {
		return err
	}
	return nil
}

func (s *NatsKVStore) GetLatestVersion(ctx context.Context, workflowName string) (string, error) {
	v := &model.WorkflowVersions{}
	if err := LoadObj(s.wfVersion, workflowName, v); err == nats.ErrKeyNotFound {
		return "", errors.ErrWorkflowNotFound
	} else if err != nil {
		return "", err
	} else {
		return v.Version[0].Id, nil
	}
}

func (s *NatsKVStore) CreateJob(ctx context.Context, job *model.WorkflowState) (string, error) {
	jobId := ksuid.New()
	job.TrackingId = jobId.String()
	if err := SaveObj(s.job, jobId.String(), job); err != nil {
		return "", err
	}
	return jobId.String(), nil
}

func (s *NatsKVStore) GetJob(ctx context.Context, id string) (*model.WorkflowState, error) {
	job := &model.WorkflowState{}
	if err := LoadObj(s.job, id, job); err != nil {
		return nil, err
	}
	return job, nil
}

func (s *NatsKVStore) ListWorkflowInstance(workflowName string) (chan *model.ListWorkflowInstanceResult, chan error) {
	errs := make(chan error, 1)
	wch := make(chan *model.ListWorkflowInstanceResult, 100)

	wfv := &model.WorkflowVersions{}
	if err := LoadObj(s.wfVersion, workflowName, wfv); err != nil {
		errs <- err
		return wch, errs
	}

	ver := make(map[string]*model.WorkflowVersion)
	for _, v := range wfv.Version {
		ver[v.Id] = v
	}

	keys, err := s.wfInstance.Keys()
	if err == nats.ErrNoKeysFound {
		keys = []string{}
	} else if err != nil {
		s.log.Error("error obtaining keys", zap.Error(err))
		return nil, errs
	}
	go func(keys []string) {
		for _, k := range keys {
			v := &model.WorkflowInstance{}
			err := LoadObj(s.wfInstance, k, v)
			if wv, ok := ver[v.WorkflowId]; ok {
				if err != nil && err != nats.ErrKeyNotFound {
					errs <- err
					s.log.Error("error loading object", zap.Error(err))
					close(errs)
					return
				}
				wch <- &model.ListWorkflowInstanceResult{
					Id:      k,
					Version: wv.Number,
				}
			}
		}
		close(wch)
	}(keys)
	return wch, errs
}

func (s *NatsKVStore) GetWorkflowInstanceStatus(id string) (*model.WorkflowInstanceStatus, error) {
	v := &model.WorkflowState{}
	err := LoadObj(s.wfTracking, id, v)
	if err != nil {
		return nil, err
	}
	return &model.WorkflowInstanceStatus{State: []*model.WorkflowState{v}}, nil
}
