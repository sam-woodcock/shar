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
	"gitlab.com/shar-workflow/shar/common/subj"
	"gitlab.com/shar-workflow/shar/model"
	"gitlab.com/shar-workflow/shar/server/errors"
	"gitlab.com/shar-workflow/shar/server/errors/keys"
	"gitlab.com/shar-workflow/shar/server/messages"
	"gitlab.com/shar-workflow/shar/server/vars"
	"go.uber.org/zap"
	"google.golang.org/protobuf/proto"
	"strconv"
	"strings"
	"sync"
	"time"
)

type NatsService struct {
	js                        nats.JetStreamContext
	txJS                      nats.JetStreamContext
	messageCompleteProcessor  MessageCompleteProcessorFunc
	eventProcessor            EventProcessorFunc
	eventJobCompleteProcessor CompleteJobProcessorFunc
	traverslFunc              TraversalFunc
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
	wfVarState                nats.KeyValue
	wf                        nats.KeyValue
	wfVersion                 nats.KeyValue
	wfTracking                nats.KeyValue
	job                       nats.KeyValue
	ownerName                 nats.KeyValue
	ownerId                   nats.KeyValue
	wfClientTask              nats.KeyValue
	conn                      common.NatsConn
	txConn                    common.NatsConn
	workflowStats             *model.WorkflowStats
	statsMx                   sync.Mutex
	wfName                    nats.KeyValue
	publishTimeout            time.Duration
}

func (s *NatsService) WorkflowStats() *model.WorkflowStats {
	s.statsMx.Lock()
	defer s.statsMx.Unlock()
	return s.workflowStats
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

func NewNatsService(log *zap.Logger, conn common.NatsConn, txConn common.NatsConn, storageType nats.StorageType, concurrency int) (*NatsService, error) {
	if concurrency < 1 || concurrency > 200 {
		return nil, errors2.New("invalid concurrency set")
	}

	js, err := conn.JetStream()
	if err != nil {
		return nil, fmt.Errorf("failed to open jetstream: %w", err)
	}
	txJS, err := txConn.JetStream()
	if err != nil {
		return nil, fmt.Errorf("failed to open jetstream: %w", err)
	}
	ms := &NatsService{
		conn:           conn,
		txConn:         txConn,
		js:             js,
		txJS:           txJS,
		log:            log,
		concurrency:    concurrency,
		storageType:    storageType,
		closing:        make(chan struct{}),
		workflowStats:  &model.WorkflowStats{},
		publishTimeout: time.Second * 30,
	}

	if err := common.SetUpNats(js, storageType); err != nil {
		return nil, fmt.Errorf("failed to set up nats queue insfrastructure: %w", err)
	}

	kvs := make(map[string]*nats.KeyValue)

	kvs[messages.KvWfName] = &ms.wfName
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
	kvs[messages.KvOwnerID] = &ms.ownerId
	kvs[messages.KvOwnerName] = &ms.ownerName
	kvs[messages.KvClientTaskID] = &ms.wfClientTask
	kvs[messages.KvVarState] = &ms.wfVarState
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

	// get this workflow name if it has already been registered
	_, err := s.wfName.Get(wf.Name)
	if err == nats.ErrKeyNotFound {
		wfNameID := ksuid.New().String()
		_, err = s.wfName.Put(wf.Name, []byte(wfNameID))
		if err != nil {
			return "", err
		}
	} else if err != nil {
		return "", err
	}

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
		if err := common.Save(s.wfClientTask, wf.Name+"_"+m.Name, []byte(ks.String())); err != nil {
			return "", err
		}

		jxCfg := &nats.ConsumerConfig{
			Durable:       "ServiceTask_" + ks.String(),
			Description:   "",
			FilterSubject: subj.SubjNS(messages.WorkflowJobSendMessageExecute, "default") + "." + ks.String(),
			AckPolicy:     nats.AckExplicitPolicy,
		}
		if err = common.EnsureConsumer(s.js, "WORKFLOW", jxCfg); err != nil {
			return "", fmt.Errorf("failed to add service task consumer: %w", err)
		}
	}
	for _, i := range wf.Process {
		for _, j := range i.Elements {
			if j.Type == "serviceTask" {
				id := ksuid.New().String()
				_, err := s.wfClientTask.Get(j.Execute)
				if err != nil && err.Error() == "nats: key not found" {
					_, err := s.wfClientTask.Put(j.Execute, []byte(id))
					if err != nil {
						return "", fmt.Errorf("failed to add task to registry: %w", err)
					}

					jxCfg := &nats.ConsumerConfig{
						Durable:       "ServiceTask_" + id,
						Description:   "",
						FilterSubject: subj.SubjNS(messages.WorkflowJobServiceTaskExecute, "default") + "." + id,
						AckPolicy:     nats.AckExplicitPolicy,
					}

					if err = common.EnsureConsumer(s.js, "WORKFLOW", jxCfg); err != nil {
						return "", fmt.Errorf("failed to add service task consumer: %w", err)
					}
				}
			}
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

	go s.incrementWorkflowCount()

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
	s.incrementWorkflowStarted()
	return wfInstance, nil
}

func (s *NatsService) GetWorkflowInstance(_ context.Context, workflowInstanceId string) (*model.WorkflowInstance, error) {
	wfi := &model.WorkflowInstance{}
	if err := common.LoadObj(s.wfInstance, workflowInstanceId, wfi); errors2.Is(err, nats.ErrKeyNotFound) {
		return nil, errors.ErrWorkflowInstanceNotFound
	} else if err != nil {
		return nil, fmt.Errorf("failed to load workflow instance from KV: %w", err)
	}
	return wfi, nil
}

func (s *NatsService) GetServiceTaskRoutingKey(taskName string) (string, error) {
	var b []byte
	var err error
	if b, err = common.Load(s.wfClientTask, taskName); err != nil {
		return "", fmt.Errorf("failed attept to get service task key: %w", err)
	}
	return string(b), nil
}

func (s *NatsService) GetMessageSenderRoutingKey(workflowName string, messageName string) (string, error) {
	_, err := s.wfName.Get(workflowName)
	if err != nil {
		return "", fmt.Errorf("cannot locate workflow: %w", err)
	}
	var b []byte
	if b, err = common.Load(s.wfClientTask, workflowName+"_"+messageName); err != nil {
		return "", fmt.Errorf("failed attept to get service task key: %w", err)
	}
	return string(b), nil
}

func (s *NatsService) DestroyWorkflowInstance(ctx context.Context, workflowInstanceId string, state model.CancellationState, wfError *model.Error) error {
	// Get the workflow instance
	wfi := &model.WorkflowInstance{}
	if err := common.LoadObj(s.wfInstance, workflowInstanceId, wfi); err != nil {
		s.log.Warn("Could not fetch workflow instance",
			zap.String(keys.WorkflowInstanceID, workflowInstanceId),
		)
		return s.expectPossibleMissingKey("error fetching workflow instance", err)
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
		s.log.Debug("Could not fetch message subscribers",
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
				s.log.Debug("could not delete instance subscriber",
					zap.String("inst.id", subs.List[i]),
				)
			}
		}
		return subs, nil
	}); err != nil {
		return s.expectPossibleMissingKey("could not update message subscriptions", err)
	}

	if err := s.wfInstance.Delete(workflowInstanceId); err != nil {
		return s.expectPossibleMissingKey("could not delete workflow instance", err)
	}

	if err := s.wfMsgSubs.Delete(workflowInstanceId); err != nil {
		return s.expectPossibleMissingKey("could not delete message subscriptions", err)
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
	s.incrementWorkflowCompleted()
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

	go s.processTraversals(ctx)
	go s.processTracking(ctx)
	go s.processWorkflowEvents(ctx)
	go s.processMessages(ctx)
	go s.listenForTimer(ctx, s.js, s.log, s.closing, 4)
	go s.processCompletedJobs(ctx)
	go s.processCompletedActivities(ctx)
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

func (s *NatsService) SetTraversalProvider(provider TraversalFunc) {
	s.traverslFunc = provider
}
func (s *NatsService) PublishWorkflowState(ctx context.Context, stateName string, state *model.WorkflowState, embargo int) error {
	state.UnixTimeNano = time.Now().UnixNano()
	msg := nats.NewMsg(subj.SubjNS(stateName, "default"))
	msg.Header.Set("embargo", strconv.Itoa(embargo))
	if b, err := proto.Marshal(state); err != nil {
		return err
	} else {
		msg.Data = b
	}
	pubCtx, cancel := context.WithTimeout(ctx, s.publishTimeout)
	defer cancel()
	id := ksuid.New().String()
	if _, err := s.txJS.PublishMsg(msg, nats.Context(pubCtx), nats.MsgId(id)); err != nil {
		s.log.Error("failed to publish message", zap.Error(err), zap.String("nats.msg.id", id), zap.Any("state", state), zap.String("subject", msg.Subject))
		return err
	}
	if stateName == subj.SubjNS(messages.WorkflowJobUserTaskExecute, "default") {
		for _, i := range append(state.Owners, state.Groups...) {
			if err := s.OpenUserTask(ctx, i, state.Id); err != nil {
				return err
			}
		}
	}
	return nil
}

func (s *NatsService) PublishMessage(ctx context.Context, workflowInstanceID string, name string, key string, vars []byte) error {
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
	msg := nats.NewMsg(fmt.Sprintf(messages.WorkflowMessageFormat, "default", workflowInstanceID, messageID))
	if b, err := proto.Marshal(sharMsg); err != nil {
		return err
	} else {
		msg.Data = b
	}
	pubCtx, cancel := context.WithTimeout(ctx, s.publishTimeout)
	defer cancel()
	id := ksuid.New().String()
	if _, err := s.txJS.PublishMsg(msg, nats.Context(pubCtx), nats.MsgId(id)); err != nil {
		s.log.Error("failed to publish message", zap.Error(err), zap.String("nats.msg.id", id), zap.Any("msg", sharMsg), zap.String("subject", msg.Subject))
		return err
	}
	return nil
}

func (s *NatsService) GetElement(ctx context.Context, state *model.WorkflowState) (*model.Element, error) {
	wf := &model.Workflow{}
	if err := common.LoadObj(s.wf, state.WorkflowId, wf); err == nats.ErrKeyNotFound {
		return nil, err
	}
	els := common.ElementTable(wf)
	if el, ok := els[state.ElementId]; ok {
		return el, nil
	}
	return nil, errors.ErrElementNotFound
}

func (s *NatsService) processTraversals(ctx context.Context) {
	common.Process(ctx, s.js, s.log, "traversal", s.closing, subj.SubjNS(messages.WorkflowTraversalExecute, "default"), "Traversal", s.concurrency, func(ctx context.Context, msg *nats.Msg) (bool, error) {
		var traversal model.WorkflowState
		if err := proto.Unmarshal(msg.Data, &traversal); err != nil {
			return false, fmt.Errorf("could not unmarshal traversal proto: %w", err)
		}
		if s.eventProcessor != nil {
			if err := s.eventProcessor(ctx, &traversal, false); errors.IsWorkflowFatal(err) {
				s.log.Error("workflow fatally terminated whilst processing activity", zap.String(keys.WorkflowInstanceID, traversal.WorkflowInstanceId), zap.String(keys.WorkflowID, traversal.WorkflowId), zap.Error(err), zap.String(keys.ElementID, traversal.ElementId))
				return true, nil
			} else if err != nil {
				return false, fmt.Errorf("could not process event: %w", err)
			}
		}
		return true, nil
	})
}

func (s *NatsService) processTracking(ctx context.Context) {
	common.Process(ctx, s.js, s.log, "tracking", s.closing, "WORKFLOW.>", "Tracking", 1, s.track)
}

func (s *NatsService) processCompletedJobs(ctx context.Context) {
	common.Process(ctx, s.js, s.log, "completedJob", s.closing, subj.SubjNS(messages.WorkFlowJobCompleteAll, "default"), "JobCompleteConsumer", s.concurrency, func(ctx context.Context, msg *nats.Msg) (bool, error) {
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
	case subj.SubjNS(messages.WorkflowInstanceExecute, "default"),
		subj.SubjNS(messages.WorkflowTraversalExecute, "default"),
		subj.SubjNS(messages.WorkflowActivityExecute, "default"),
		subj.SubjNS(messages.WorkflowJobServiceTaskExecute, "default"),
		subj.SubjNS(messages.WorkflowJobManualTaskExecute, "default"),
		subj.SubjNS(messages.WorkflowJobUserTaskExecute, "default"):
		st := &model.WorkflowState{}
		if err := proto.Unmarshal(msg.Data, st); err != nil {
			return false, err
		}
		if err := common.SaveObj(ctx, kv, st.WorkflowInstanceId, st); err != nil {
			return false, err
		}
	case subj.SubjNS(messages.WorkflowInstanceComplete, "default"):
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

//nolint:ireturn
func (s *NatsService) Conn() common.NatsConn {
	return s.conn
}

func (s *NatsService) processMessages(ctx context.Context) {
	common.Process(ctx, s.js, s.log, "message", s.closing, subj.SubjNS(messages.WorkflowMessages, "*"), "Message", s.concurrency, s.processMessage)
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
	workflowInstanceId := subj[3]
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
			return false, &errors.ErrWorkflowFatal{Err: err}
		}
		if !success {
			continue
		}

		el, err := s.GetElement(ctx, sub)
		if err != nil {
			return true, &errors.ErrWorkflowFatal{Err: err}
		}

		err = vars.OutputVars(s.log, sub, el)
		if err != nil {
			return false, err
		}

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
	common.Process(ctx, s.js, s.log, "workflowEvent", s.closing, subj.SubjNS(messages.WorkflowInstanceAll, "default"), "WorkflowConsumer", s.concurrency, func(ctx context.Context, msg *nats.Msg) (bool, error) {
		var job model.WorkflowState
		if err := proto.Unmarshal(msg.Data, &job); err != nil {
			return false, err
		}
		if strings.HasSuffix(msg.Subject, ".State.Workflow.Complete") {
			if err := s.DestroyWorkflowInstance(ctx, job.WorkflowInstanceId, job.State, job.Error); err != nil {
				return false, err
			}
		}
		return true, nil
	})
}

func (s *NatsService) processCompletedActivities(ctx context.Context) {
	common.Process(ctx, s.js, s.log, "completedActivity", s.closing, subj.SubjNS(messages.WorkflowActivityComplete, "default"), "ActivityCompleteConsumer", s.concurrency, func(ctx context.Context, msg *nats.Msg) (bool, error) {
		var job model.WorkflowState
		if err := proto.Unmarshal(msg.Data, &job); err != nil {
			return false, err
		}
		fmt.Println("**"+job.ElementId, job.WorkflowInstanceId, job.Id, job.ParentId)
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

func (s *NatsService) incrementWorkflowCount() {
	s.statsMx.Lock()
	s.workflowStats.Workflows++
	s.statsMx.Unlock()
}

func (s *NatsService) incrementWorkflowCompleted() {
	s.statsMx.Lock()
	s.workflowStats.InstancesComplete++
	s.statsMx.Unlock()
}

func (s *NatsService) incrementWorkflowStarted() {
	s.statsMx.Lock()
	s.workflowStats.InstancesStarted++
	s.statsMx.Unlock()
}

func (s *NatsService) expectPossibleMissingKey(msg string, err error) error {
	if errors2.Is(err, nats.ErrKeyNotFound) {
		s.log.Debug(msg, zap.Error(err))
		return nil
	}
	return err
}

func (s *NatsService) listenForTimer(ctx context.Context, js nats.JetStreamContext, log *zap.Logger, closer chan struct{}, concurrency int) {
	subject := subj.SubjNS(messages.WorkflowTimedExecute, "*")
	durable := "workflowTimers"
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

				state := &model.WorkflowState{}
				err = proto.Unmarshal(msg[0].Data, state)
				if err != nil {
					s.log.Error("could not unmarshal timer proto: %s", zap.Error(err))
					err := msg[0].Ack()
					if err != nil {
						s.log.Error("could not dispose of timer message after unmarshal error: %s", zap.Error(err))
					}
					return
				}

				wf, err := s.GetWorkflow(ctx, state.WorkflowId)
				if err != nil {
					s.log.Error("could not get timer proto workflow: %s", zap.Error(err))
					err := msg[0].Ack()
					if err != nil {
						s.log.Error("could not dispose of timer message after faliure to obtain workflow: %s", zap.Error(err))
					}
					return
				}

				els := common.ElementTable(wf)
				el := els[state.ElementId]

				now := time.Now().UnixNano()
				elapsed := now - state.Timer.LastFired
				count := state.Timer.Count
				repeat := el.Timer.Repeat
				value := el.Timer.Value

				newTimer := &model.WorkflowState{
					WorkflowId: state.WorkflowId,
					ElementId:  state.ElementId,
					Timer: &model.WorkflowTimer{
						LastFired: now,
						Count:     count + 1,
					},
				}

				switch el.Timer.Type {
				case model.WorkflowTimerType_fixed:
					if value <= now {
						wfi, err := s.CreateWorkflowInstance(ctx, &model.WorkflowInstance{
							WorkflowId: state.WorkflowId,
						})
						if err != nil {
							s.log.Error("error creating timed workflow instance", zap.Error(err))
							return
						}
						if err := s.traverslFunc(ctx, wfi, ksuid.New().String(), el.Outbound, els, state.Vars); err != nil {
							s.log.Error("error traversing for timed workflow instance", zap.Error(err))
							return
						}
						if err := s.PublishWorkflowState(ctx, messages.WorkflowTimedExecute, newTimer, 0); err != nil {
							s.log.Error("error publishing timer", zap.Error(err))
							return
						}
					} else {
						if err := msg[0].NakWithDelay(time.Duration(value - now)); err != nil {
							s.log.Error("error backing off timer", zap.Error(err))
							return
						}
						return
					}
					if err := msg[0].Ack(); err != nil {
						s.log.Error("error acknowledging timer", zap.Error(err))
						return
					}
				case model.WorkflowTimerType_duration:
					if repeat == 0 || count < repeat {
						if elapsed >= value {
							wfi, err := s.CreateWorkflowInstance(ctx, &model.WorkflowInstance{
								WorkflowId: state.WorkflowId,
							})
							if err != nil {
								s.log.Error("error creating timed workflow instance", zap.Error(err))
								return
							}
							if err := s.traverslFunc(ctx, wfi, ksuid.New().String(), el.Outbound, els, state.Vars); err != nil {
								s.log.Error("error traversing for timed workflow instance", zap.Error(err))
								return
							}
							if err := s.PublishWorkflowState(ctx, messages.WorkflowTimedExecute, newTimer, 0); err != nil {
								s.log.Error("error publishing timer", zap.Error(err))
								return
							}
						} else {
							if err := msg[0].NakWithDelay(time.Duration(value - elapsed)); err != nil {
								s.log.Error("error backing off timer", zap.Error(err))
								return
							}
							return
						}
					}
					if err := msg[0].Ack(); err != nil {
						s.log.Error("error acknowledging timer", zap.Error(err))
						return
					}
				}
			}
		}()
	}
}

func (s *NatsService) SaveVariableState(_ context.Context, key string, vars []byte) error {
	return common.Save(s.wfVarState, key, vars)
}
func (s *NatsService) LoadVariableState(_ context.Context, key string) ([]byte, error) {
	ret, err := common.Load(s.wfVarState, key)
	if err != nil {
		return nil, err
	}
	return ret, nil
}
func (s *NatsService) DeleteVariableState(_ context.Context, key string) error {
	return common.Delete(s.wfVarState, key)
}
