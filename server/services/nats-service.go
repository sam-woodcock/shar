package services

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	errors2 "errors"
	"fmt"
	"github.com/nats-io/nats.go"
	"github.com/segmentio/ksuid"
	"gitlab.com/shar-workflow/shar/common"
	"gitlab.com/shar-workflow/shar/common/expression"
	"gitlab.com/shar-workflow/shar/model"
	"gitlab.com/shar-workflow/shar/server/errors"
	"gitlab.com/shar-workflow/shar/server/errors/keys"
	"gitlab.com/shar-workflow/shar/server/messages"
	"gitlab.com/shar-workflow/shar/server/vars"
	"go.uber.org/zap"
	"google.golang.org/protobuf/proto"
	"strconv"
	"strings"
	"time"
)

type NatsService struct {
	js                        nats.JetStreamContext
	messageCompleteProcessor  MessageCompleteProcessorFunc
	eventProcessor            EventProcessorFunc
	eventJobCompleteProcessor CompleteJobProcessorFunc
	log                       *zap.Logger
	storageType               nats.StorageType
	concurrency               int
	closing                   chan struct{}
	wfMsgSubs                 nats.KeyValue
	wfMsgSub                  nats.KeyValue
	wfInstance                nats.KeyValue
	wfMessageName             nats.KeyValue
	wfMessageID               nats.KeyValue
	wfUserTasks               nats.KeyValue
	wf                        nats.KeyValue
	wfVersion                 nats.KeyValue
	wfTracking                nats.KeyValue
	job                       nats.KeyValue
	ownerName                 nats.KeyValue
	ownerId                   nats.KeyValue
	conn                      common.NatsConn
}

func (s *NatsService) AwaitMsg(ctx context.Context, state *model.WorkflowState) error {
	id := ksuid.New().String()
	if err := common.SaveObj(ctx, s.wfMsgSub, id, state); err != nil {
		return err
	}
	if err := common.UpdateObj(ctx, s.wfMsgSubs, state.WorkflowInstanceId, &model.WorkflowInstanceSubscribers{}, func(v *model.WorkflowInstanceSubscribers) (*model.WorkflowInstanceSubscribers, error) {
		v.List = append(v.List, id)
		return v, nil
	}); err != nil {
		return err
	}
	return nil
}

func (s *NatsService) ListWorkflows(_ context.Context) (chan *model.ListWorkflowResult, chan error) {
	res := make(chan *model.ListWorkflowResult, 100)
	errs := make(chan error, 1)
	ks, err := s.wfVersion.Keys()
	if err == nats.ErrNoKeysFound {
		ks = []string{}
	} else if err != nil {
		errs <- err
		return res, errs
	}
	go func() {
		for _, k := range ks {
			v := &model.WorkflowVersions{}
			err := common.LoadObj(s.wfVersion, k, v)
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

func NewNatsService(log *zap.Logger, conn common.NatsConn, storageType nats.StorageType, concurrency int) (*NatsService, error) {
	if concurrency < 1 || concurrency > 200 {
		return nil, errors2.New("invalid concurrency set")
	}

	js, err := conn.JetStream()
	if err != nil {
		return nil, fmt.Errorf("failed to open jetstream: %w", err)
	}
	ms := &NatsService{
		conn:        conn,
		js:          js,
		log:         log,
		concurrency: concurrency,
		storageType: storageType,
		closing:     make(chan struct{}),
	}
	kvs := make(map[string]*nats.KeyValue)

	kvs[messages.KvInstance] = &ms.wfInstance
	kvs[messages.KvTracking] = &ms.wfTracking
	kvs[messages.KvDefinition] = &ms.wf
	kvs[messages.KvJob] = &ms.job
	kvs[messages.KvVersion] = &ms.wfVersion
	kvs[messages.KvMessageSubs] = &ms.wfMsgSubs
	kvs[messages.KvMessageSub] = &ms.wfMsgSub
	kvs[messages.KvMessageName] = &ms.wfMessageName
	kvs[messages.KvMessageID] = &ms.wfMessageID
	kvs[messages.KvUserTask] = &ms.wfUserTasks
	kvs[messages.KvOwnerId] = &ms.ownerId
	kvs[messages.KvOwnerName] = &ms.ownerName
	ks := make([]string, 0, len(kvs))
	for k := range kvs {
		ks = append(ks, k)
	}
	if err := common.EnsureBuckets(js, storageType, ks); err != nil {
		return nil, fmt.Errorf("failed to ensure the KV buckets: %w", err)
	}

	for k, v := range kvs {
		if kv, err := js.KeyValue(k); err != nil {
			return nil, fmt.Errorf("failed to open %s KV: %w", k, err)
		} else {
			*v = kv
		}
	}

	return ms, nil
}

func (s *NatsService) StoreWorkflow(ctx context.Context, wf *model.Workflow) (string, error) {
	wfID := ksuid.New().String()
	b, err := json.Marshal(wf)
	if err != nil {
		return "", err
	}
	h := sha256.New()
	if _, err := h.Write(b); err != nil {
		return "", fmt.Errorf("could not marshal workflow: %s", wf.Name)
	}
	hash := h.Sum(nil)
	err = common.SaveObj(ctx, s.wf, wfID, wf)
	if err != nil {
		return "", fmt.Errorf("could not save workflow: %s", wf.Name)
	}
	for _, m := range wf.Messages {
		ks := ksuid.New()
		if _, err := common.Load(s.wfMessageID, m.Name); err == nil {
			continue
		}
		if err := common.Save(s.wfMessageID, m.Name, []byte(ks.String())); err != nil {
			return "", err
		}
		if err := common.Save(s.wfMessageName, ks.String(), []byte(m.Name)); err != nil {
			return "", err
		}
	}
	if err := common.UpdateObj(ctx, s.wfVersion, wf.Name, &model.WorkflowVersions{}, func(v *model.WorkflowVersions) (*model.WorkflowVersions, error) {
		if v.Version == nil || len(v.Version) == 0 {
			v.Version = make([]*model.WorkflowVersion, 0, 1)
		} else {
			if bytes.Equal(hash, v.Version[0].Sha256) {
				return v, nil
			}
		}
		v.Version = append([]*model.WorkflowVersion{
			{Id: wfID, Sha256: hash, Number: int32(len(v.Version)) + 1},
		}, v.Version...)
		return v, nil
	}); err != nil {
		return "", fmt.Errorf("could not update workflow version for: %s", wf.Name)
	}
	return wfID, nil
}

func (s *NatsService) GetWorkflow(_ context.Context, workflowId string) (*model.Workflow, error) {
	wf := &model.Workflow{}
	if err := common.LoadObj(s.wf, workflowId, wf); err == nats.ErrKeyNotFound {
		return nil, errors.ErrWorkflowNotFound
	} else if err != nil {
		return nil, fmt.Errorf("failed to load workflow from KV: %w", err)
	}
	return wf, nil
}

func (s *NatsService) CreateWorkflowInstance(ctx context.Context, wfInstance *model.WorkflowInstance) (*model.WorkflowInstance, error) {
	wfiID := ksuid.New().String()
	wfInstance.WorkflowInstanceId = wfiID
	if err := common.SaveObj(ctx, s.wfInstance, wfiID, wfInstance); err != nil {
		return nil, fmt.Errorf("failed to save workflow instance object to KV: %w", err)
	}
	subs := &model.WorkflowInstanceSubscribers{List: []string{}}
	if err := common.SaveObj(ctx, s.wfMsgSubs, wfiID, subs); err != nil {
		return nil, fmt.Errorf("failed to save workflow instance object to KV: %w", err)
	}
	return wfInstance, nil
}

func (s *NatsService) GetWorkflowInstance(_ context.Context, workflowInstanceId string) (*model.WorkflowInstance, error) {
	wfi := &model.WorkflowInstance{}
	if err := common.LoadObj(s.wfInstance, workflowInstanceId, wfi); err == nats.ErrKeyNotFound {
		return nil, errors.ErrWorkflowNotFound
	} else if err != nil {
		return nil, fmt.Errorf("failed to load workflow instance from KV: %w", err)
	}
	return wfi, nil
}

func (s *NatsService) DestroyWorkflowInstance(ctx context.Context, workflowInstanceId string, state model.CancellationState, wfError *model.Error) error {
	// Get the workflow instance
	wfi := &model.WorkflowInstance{}
	if err := common.LoadObj(s.wfInstance, workflowInstanceId, wfi); err != nil {
		s.log.Warn("Could not fetch workflow instance",
			zap.String(keys.WorkflowInstanceID, workflowInstanceId),
		)
		if errors2.Is(err, nats.ErrKeyNotFound) {
			return nil
		}
		return err
	}

	// Get the workflow
	wf := &model.Workflow{}
	if wfi.WorkflowId != "" {
		if err := common.LoadObj(s.wf, wfi.WorkflowId, wf); err != nil {
			s.log.Warn("Could not fetch workflow definition",
				zap.String(keys.WorkflowInstanceID, wfi.WorkflowInstanceId),
				zap.String(keys.WorkflowID, wfi.WorkflowId),
				zap.String(keys.WorkflowName, wf.Name),
			)
		}
	}

	// Get all the subscriptions
	subs := &model.WorkflowInstanceSubscribers{}
	if err := common.LoadObj(s.wfMsgSubs, wfi.WorkflowInstanceId, subs); err != nil {
		s.log.Warn("Could not fetch message subscribers",
			zap.String(keys.WorkflowInstanceID, wfi.WorkflowInstanceId),
			zap.String(keys.WorkflowID, wfi.WorkflowId),
			zap.String(keys.WorkflowName, wf.Name),
			zap.String(keys.MessageID, wf.Name),
		)
	}

	if err := common.UpdateObj(ctx, s.wfMsgSubs, workflowInstanceId, &model.WorkflowInstanceSubscribers{}, func(subs *model.WorkflowInstanceSubscribers) (*model.WorkflowInstanceSubscribers, error) {
		for i := 0; i < len(subs.List); i++ {
			err := s.wfMsgSub.Delete(subs.List[i])
			if err != nil {
				s.log.Warn("Could not delete instance subscriber",
					zap.String("inst.id", subs.List[i]),
				)
			}
		}
		return subs, nil
	}); err != nil {
		return err
	}

	if err := s.wfInstance.Delete(workflowInstanceId); err != nil {
		return err
	}

	if err := s.wfMsgSubs.Delete(workflowInstanceId); err != nil {
		return err
	}

	tState := &model.WorkflowState{
		WorkflowId:         wf.Name,
		WorkflowInstanceId: wfi.WorkflowInstanceId,
		State:              state,
		Error:              wfError,
		UnixTimeNano:       time.Now().UnixNano(),
	}

	if tState.Error != nil {
		tState.State = model.CancellationState_Errored
	}

	if err := s.PublishWorkflowState(ctx, messages.WorkflowInstanceTerminated, tState, 0); err != nil {
		return err
	}

	return nil
}

// GetLatestVersion queries the workflow versions table for the latest entry
func (s *NatsService) GetLatestVersion(_ context.Context, workflowName string) (string, error) {
	v := &model.WorkflowVersions{}
	if err := common.LoadObj(s.wfVersion, workflowName, v); err == nats.ErrKeyNotFound {
		return "", errors.ErrWorkflowNotFound
	} else if err != nil {
		return "", err
	} else {
		return v.Version[0].Id, nil
	}
}

func (s *NatsService) CreateJob(ctx context.Context, job *model.WorkflowState) (string, error) {
	tid := ksuid.New().String()
	job.Id = tid
	if err := common.SaveObj(ctx, s.job, tid, job); err != nil {
		return "", fmt.Errorf("failed to save job to KV: %w", err)
	}
	return tid, nil
}

func (s *NatsService) GetJob(_ context.Context, trackingID string) (*model.WorkflowState, error) {
	job := &model.WorkflowState{}
	if err := common.LoadObj(s.job, trackingID, job); err != nil {
		return nil, fmt.Errorf("failed to load job from KV: %w", err)
	}
	return job, nil
}

func (s *NatsService) ListWorkflowInstance(_ context.Context, workflowName string) (chan *model.ListWorkflowInstanceResult, chan error) {
	errs := make(chan error, 1)
	wch := make(chan *model.ListWorkflowInstanceResult, 100)

	wfv := &model.WorkflowVersions{}
	if err := common.LoadObj(s.wfVersion, workflowName, wfv); err != nil {
		errs <- err
		return wch, errs
	}

	ver := make(map[string]*model.WorkflowVersion)
	for _, v := range wfv.Version {
		ver[v.Id] = v
	}

	ks, err := s.wfInstance.Keys()
	if err == nats.ErrNoKeysFound {
		ks = []string{}
	} else if err != nil {
		s.log.Error("error obtaining keys", zap.Error(err))
		return nil, errs
	}
	go func(keys []string) {
		for _, k := range keys {
			v := &model.WorkflowInstance{}
			err := common.LoadObj(s.wfInstance, k, v)
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
	}(ks)
	return wch, errs
}

func (s *NatsService) GetWorkflowInstanceStatus(_ context.Context, id string) (*model.WorkflowInstanceStatus, error) {
	v := &model.WorkflowState{}
	err := common.LoadObj(s.wfTracking, id, v)
	if err != nil {
		return nil, fmt.Errorf("failed to load workflow instance status from KV: %w", err)
	}
	return &model.WorkflowInstanceStatus{State: []*model.WorkflowState{v}}, nil
}

func (s *NatsService) StartProcessing(ctx context.Context) error {

	if err := common.SetUpNats(s.js, s.storageType); err != nil {
		return err
	}

	go func() {
		s.processTraversals(ctx)
	}()

	go func() {
		s.processTracking(ctx)
	}()

	go func() {
		s.processWorkflowEvents(ctx)
	}()

	go func() {
		s.processMessages(ctx)
	}()

	go s.processCompletedJobs(ctx)
	return nil
}
func (s *NatsService) SetEventProcessor(processor EventProcessorFunc) {
	s.eventProcessor = processor
}
func (s *NatsService) SetMessageCompleteProcessor(processor MessageCompleteProcessorFunc) {
	s.messageCompleteProcessor = processor
}
func (s *NatsService) SetCompleteJobProcessor(processor CompleteJobProcessorFunc) {
	s.eventJobCompleteProcessor = processor
}

func (s *NatsService) PublishWorkflowState(ctx context.Context, stateName string, state *model.WorkflowState, embargo int) error {
	state.UnixTimeNano = time.Now().UnixNano()
	msg := nats.NewMsg(stateName)
	msg.Header.Set("embargo", strconv.Itoa(embargo))
	if b, err := proto.Marshal(state); err != nil {
		return err
	} else {
		msg.Data = b
	}
	ctx, cancel := context.WithTimeout(ctx, time.Second*30)
	defer cancel()
	if _, err := s.js.PublishMsg(msg, nats.Context(ctx)); err != nil {
		return err
	}
	if stateName == messages.WorkflowJobUserTaskExecute {
		for _, i := range append(state.Owners, state.Groups...) {
			if err := s.OpenUserTask(ctx, i, state.Id); err != nil {
				return err
			}
		}
	}
	return nil
}

func (s *NatsService) PublishMessage(_ context.Context, workflowInstanceID string, name string, key string, vars []byte) error {
	messageIDb, err := common.Load(s.wfMessageID, name)
	messageID := string(messageIDb)
	if err != nil {
		return err
	}
	sharMsg := &model.MessageInstance{
		MessageId:      messageID,
		CorrelationKey: key,
		Vars:           vars,
	}
	msg := nats.NewMsg(fmt.Sprintf(messages.WorkflowMessageFormat, workflowInstanceID, messageID))
	if b, err := proto.Marshal(sharMsg); err != nil {
		return err
	} else {
		msg.Data = b
	}
	if _, err := s.js.PublishMsg(msg); err != nil {
		return err
	}
	return nil
}

func (s *NatsService) processTraversals(ctx context.Context) {
	common.Process(ctx, s.js, s.log, s.closing, messages.WorkflowTraversalExecute, "Traversal", s.concurrency, func(ctx context.Context, msg *nats.Msg) (bool, error) {
		var traversal model.WorkflowState
		if err := proto.Unmarshal(msg.Data, &traversal); err != nil {
			return false, fmt.Errorf("could not unmarshal traversal proto: %w", err)
		}
		if s.eventProcessor != nil {
			if err := s.eventProcessor(ctx, &traversal, false); err != nil {
				return false, fmt.Errorf("could not process event: %w", err)
			}
		}
		return true, nil
	})
}

func (s *NatsService) processTracking(ctx context.Context) {
	common.Process(ctx, s.js, s.log, s.closing, "WORKFLOW.>", "Tracking", 1, s.track)
}

func (s *NatsService) processCompletedJobs(ctx context.Context) {
	common.Process(ctx, s.js, s.log, s.closing, messages.WorkFlowJobCompleteAll, "JobCompleteConsumer", s.concurrency, func(ctx context.Context, msg *nats.Msg) (bool, error) {
		var job model.WorkflowState
		if err := proto.Unmarshal(msg.Data, &job); err != nil {
			return false, err
		}
		if s.eventJobCompleteProcessor != nil {
			if err := s.eventJobCompleteProcessor(ctx, job.Id, job.Vars); err != nil {
				return false, err
			}
		}
		return true, nil
	})
}

func (s *NatsService) track(ctx context.Context, msg *nats.Msg) (bool, error) {
	kv, err := s.js.KeyValue(messages.KvTracking)
	if err != nil {
		return false, err
	}
	switch msg.Subject {
	case messages.WorkflowInstanceExecute,
		messages.WorkflowTraversalExecute,
		messages.WorkflowActivityExecute,
		messages.WorkflowJobServiceTaskExecute,
		messages.WorkflowJobManualTaskExecute,
		messages.WorkflowJobUserTaskExecute:
		st := &model.WorkflowState{}
		if err := proto.Unmarshal(msg.Data, st); err != nil {
			return false, err
		}
		if err := common.SaveObj(ctx, kv, st.WorkflowInstanceId, st); err != nil {
			return false, err
		}
	case messages.WorkflowInstanceComplete:
		st := &model.WorkflowState{}
		if err := proto.Unmarshal(msg.Data, st); err != nil {
			return false, err
		}
		if err := kv.Delete(st.WorkflowInstanceId); err != nil {
			return false, err
		}
	}
	return true, nil
}

func (s *NatsService) Conn() common.NatsConn {
	return s.conn
}

func (s *NatsService) processMessages(ctx context.Context) {
	common.Process(ctx, s.js, s.log, s.closing, messages.WorkflowMessages, "Message", s.concurrency, s.processMessage)
}

func (s *NatsService) processMessage(ctx context.Context, msg *nats.Msg) (bool, error) {
	// Unpack the message Instance
	instance := &model.MessageInstance{}
	if err := proto.Unmarshal(msg.Data, instance); err != nil {
		return false, fmt.Errorf("could not unmarshal message proto: %w", err)
	}
	messageName, err := common.Load(s.wfMessageName, instance.MessageId)
	if err != nil {
		return false, fmt.Errorf("failed to load message name for message id %s: %w", instance.MessageId, err)
	}
	subj := strings.Split(msg.Subject, ".")
	if len(subj) < 4 {
		return true, nil
	}
	workflowInstanceId := subj[2]

	subs := &model.WorkflowInstanceSubscribers{}
	if err := common.LoadObj(s.wfMsgSubs, workflowInstanceId, subs); err != nil {
		return true, nil
	}
	for _, i := range subs.List {

		sub := &model.WorkflowState{}
		if err := common.LoadObj(s.wfMsgSub, i, sub); err == nats.ErrKeyNotFound {
			continue
		} else if err != nil {
			return false, err
		}
		if *sub.Condition != string(messageName) {
			continue
		}
		dv, err := vars.Decode(s.log, sub.Vars)
		if err != nil {
			return false, err
		}
		success, err := expression.Eval[bool](s.log, *sub.Execute+"=="+instance.CorrelationKey, dv)
		if err != nil {
			return false, err
		}
		if !success {
			continue
		}
		newv, err := vars.Merge(s.log, sub.Vars, instance.Vars)
		if err != nil {
			return false, err
		}
		sub.Vars = newv

		if s.messageCompleteProcessor != nil {
			if err := s.messageCompleteProcessor(ctx, sub); err != nil {
				return false, err
			}
		}
		if err := common.UpdateObj(ctx, s.wfMsgSubs, workflowInstanceId, &model.WorkflowInstanceSubscribers{}, func(v *model.WorkflowInstanceSubscribers) (*model.WorkflowInstanceSubscribers, error) {
			remove(v.List, i)
			return v, nil
		}); err != nil {
			return false, err
		}
	}
	return true, nil
}

func remove[T comparable](slice []T, member T) []T {
	for i, v := range slice {
		if v == member {
			slice = append(slice[:i], slice[i+1:]...)
			break
		}
	}
	return slice
}

func (s *NatsService) Shutdown() {
	close(s.closing)
}

func (s *NatsService) processWorkflowEvents(ctx context.Context) {
	common.Process(ctx, s.js, s.log, s.closing, messages.WorkflowInstanceAll, "WorkflowConsumer", s.concurrency, func(ctx context.Context, msg *nats.Msg) (bool, error) {
		var job model.WorkflowState
		if err := proto.Unmarshal(msg.Data, &job); err != nil {
			return false, err
		}
		if msg.Subject == "WORKFLOW.State.Workflow.Complete" {
			if err := s.DestroyWorkflowInstance(ctx, job.WorkflowInstanceId, job.State, job.Error); err != nil {
				return false, err
			}
		}
		return true, nil
	})
}

func (s *NatsService) CloseUserTask(ctx context.Context, trackingID string) error {
	job := &model.WorkflowState{}
	if err := common.LoadObj(s.job, trackingID, job); err != nil {
		return err
	}

	// TODO: abstract group and user names
	var reterr error
	allIDs := append(job.Owners, job.Groups...)
	for _, i := range allIDs {
		if err := common.UpdateObj(ctx, s.wfUserTasks, i, &model.UserTasks{}, func(msg *model.UserTasks) (*model.UserTasks, error) {
			msg.Id = remove(msg.Id, trackingID)
			return msg, nil
		}); err != nil {
			reterr = err
		}
	}
	return reterr
}

func (s *NatsService) OpenUserTask(ctx context.Context, owner string, id string) error {
	return common.UpdateObj(ctx, s.wfUserTasks, owner, &model.UserTasks{}, func(msg *model.UserTasks) (*model.UserTasks, error) {
		msg.Id = append(msg.Id, id)
		return msg, nil
	})
}

func (s *NatsService) GetUserTaskIDs(ctx context.Context, owner string) (*model.UserTasks, error) {
	ut := &model.UserTasks{}
	if err := common.LoadObj(s.wfUserTasks, owner, ut); err != nil {
		return nil, err
	}
	return ut, nil
}

func (s *NatsService) OwnerId(name string) (string, error) {
	if name == "" {
		name = "AnyUser"
	}
	nm, err := s.ownerId.Get(name)
	if err != nil && err != nats.ErrKeyNotFound {
		return "", err
	}
	if nm == nil {
		id := ksuid.New().String()
		if _, err := s.ownerId.Put(name, []byte(id)); err != nil {
			return "", err
		}
		if _, err = s.ownerName.Put(id, []byte(name)); err != nil {
			return "", err
		}
		return id, nil
	}
	return string(nm.Value()), nil
}
func (s *NatsService) OwnerName(id string) (string, error) {
	nm, err := s.ownerName.Get(id)
	if err != nil {
		return "", err
	}
	return string(nm.Value()), nil
}
