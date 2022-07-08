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
	wf                        nats.KeyValue
	wfVersion                 nats.KeyValue
	wfTracking                nats.KeyValue
	job                       nats.KeyValue
	conn                      common.NatsConn
}

func (s *NatsService) AwaitMsg(ctx context.Context, state *model.WorkflowState) error {
	id := ksuid.New().String()
	if err := common.SaveObj(ctx, s.wfMsgSub, id, state); err != nil {
		return err
	}
	if err := common.UpdateObj(s.wfMsgSubs, state.WorkflowInstanceId, &model.WorkflowInstanceSubscribers{}, func(v *model.WorkflowInstanceSubscribers) (*model.WorkflowInstanceSubscribers, error) {
		v.List = append(v.List, id)
		return v, nil
	}); err != nil {
		return err
	}
	return nil
}

func (s *NatsService) ListWorkflows(ctx context.Context) (chan *model.ListWorkflowResult, chan error) {
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
	err = common.SaveObj(nil, s.wf, wfID, wf)
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
	if err := common.UpdateObj(s.wfVersion, wf.Name, &model.WorkflowVersions{}, func(v *model.WorkflowVersions) (*model.WorkflowVersions, error) {
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
		fmt.Errorf("could not update workflow version for: %s", wf.Name)
	}
	return wfID, nil
}

func (s *NatsService) GetWorkflow(ctx context.Context, workflowId string) (*model.Workflow, error) {
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
	if err := common.SaveObj(nil, s.wfInstance, wfiID, wfInstance); err != nil {
		return nil, fmt.Errorf("failed to save workflow instance object to KV: %w", err)
	}
	subs := &model.WorkflowInstanceSubscribers{List: []string{}}
	if err := common.SaveObj(nil, s.wfMsgSubs, wfiID, subs); err != nil {
		return nil, fmt.Errorf("failed to save workflow instance object to KV: %w", err)
	}
	return wfInstance, nil
}

func (s *NatsService) GetWorkflowInstance(ctx context.Context, workflowInstanceId string) (*model.WorkflowInstance, error) {
	wfi := &model.WorkflowInstance{}
	if err := common.LoadObj(s.wfInstance, workflowInstanceId, wfi); err == nats.ErrKeyNotFound {
		return nil, errors.ErrWorkflowNotFound
	} else if err != nil {
		return nil, fmt.Errorf("failed to load workflow instance from KV: %w", err)
	}
	return wfi, nil
}

func (s *NatsService) DestroyWorkflowInstance(ctx context.Context, workflowInstanceId string) error {
	// Get the workflow instance
	wfi := &model.WorkflowInstance{}
	if err := common.LoadObj(s.wfInstance, workflowInstanceId, wfi); err != nil {
		s.log.Warn("Could not fetch workflow instance",
			zap.String(keys.WorkflowInstanceID, workflowInstanceId),
		)
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

	if err := common.UpdateObj(s.wfMsgSubs, workflowInstanceId, &model.WorkflowInstanceSubscribers{}, func(subs *model.WorkflowInstanceSubscribers) (*model.WorkflowInstanceSubscribers, error) {
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

	state := &model.WorkflowState{
		WorkflowId:         wf.Name,
		WorkflowInstanceId: wfi.WorkflowInstanceId,
		State:              "Terminated",
		UnixTimeNano:       time.Now().UnixNano(),
	}

	s.PublishWorkflowState(ctx, messages.WorkflowInstanceTerminated, state, 0)

	return nil
}

// GetLatestVersion queries the workflow versions table for the latest entry
func (s *NatsService) GetLatestVersion(ctx context.Context, workflowName string) (string, error) {
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
	jobId := ksuid.New()
	job.TrackingId = jobId.String()
	if err := common.SaveObj(nil, s.job, jobId.String(), job); err != nil {
		return "", fmt.Errorf("failed to save job to KV: %w", err)
	}
	return jobId.String(), nil
}

func (s *NatsService) GetJob(ctx context.Context, id string) (*model.WorkflowState, error) {
	job := &model.WorkflowState{}
	if err := common.LoadObj(s.job, id, job); err != nil {
		return nil, fmt.Errorf("failed to load job from KV: %w", err)
	}
	return job, nil
}

func (s *NatsService) ListWorkflowInstance(ctx context.Context, workflowName string) (chan *model.ListWorkflowInstanceResult, chan error) {
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

func (s *NatsService) GetWorkflowInstanceStatus(ctx context.Context, id string) (*model.WorkflowInstanceStatus, error) {
	v := &model.WorkflowState{}
	err := common.LoadObj(s.wfTracking, id, v)
	if err != nil {
		return nil, fmt.Errorf("failed to load workflow instance status from KV: %w", err)
	}
	return &model.WorkflowInstanceStatus{State: []*model.WorkflowState{v}}, nil
}

func (s *NatsService) StartProcessing(ctx context.Context) error {
	scfg := &nats.StreamConfig{
		Name:      "WORKFLOW",
		Subjects:  messages.AllMessages,
		Storage:   s.storageType,
		Retention: nats.InterestPolicy,
	}

	ccfg := &nats.ConsumerConfig{
		Durable:         "Traversal",
		Description:     "Traversal processing queue",
		AckPolicy:       nats.AckExplicitPolicy,
		FilterSubject:   messages.WorkflowTraversalExecute,
		MaxRequestBatch: 1,
	}

	tcfg := &nats.ConsumerConfig{
		Durable:         "Tracking",
		Description:     "Tracking queue for sequential processing",
		AckPolicy:       nats.AckExplicitPolicy,
		FilterSubject:   "WORKFLOW.>",
		MaxAckPending:   1,
		MaxRequestBatch: 1,
	}

	acfg := &nats.ConsumerConfig{
		Durable:         "API",
		Description:     "Api queue",
		AckPolicy:       nats.AckExplicitPolicy,
		FilterSubject:   messages.ApiAll,
		MaxRequestBatch: 1,
	}

	if _, err := s.js.StreamInfo(scfg.Name); err == nats.ErrStreamNotFound {
		if _, err := s.js.AddStream(scfg); err != nil {
			panic(err)
		}
	} else if err != nil {
		panic(err)
	}

	if _, err := s.js.ConsumerInfo(scfg.Name, ccfg.Durable); err == nats.ErrConsumerNotFound {
		if _, err := s.js.AddConsumer("WORKFLOW", ccfg); err != nil {
			panic(err)
		}
	} else if err != nil {
		panic(err)
	}

	if _, err := s.js.ConsumerInfo(scfg.Name, tcfg.Durable); err == nats.ErrConsumerNotFound {
		if _, err := s.js.AddConsumer("WORKFLOW", tcfg); err != nil {
			panic(err)
		}
	} else if err != nil {
		panic(err)
	}

	if _, err := s.js.ConsumerInfo(scfg.Name, acfg.Durable); err == nats.ErrConsumerNotFound {
		if _, err := s.js.AddConsumer("WORKFLOW", acfg); err != nil {
			panic(err)
		}
	} else if err != nil {
		panic(err)
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
	if _, err := s.js.PublishMsg(msg); err != nil {
		return err
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
	s.process(ctx, messages.WorkflowTraversalExecute, "Traversal", func(ctx context.Context, msg *nats.Msg) (bool, error) {
		var traversal model.WorkflowState
		if err := proto.Unmarshal(msg.Data, &traversal); err != nil {
			return false, fmt.Errorf("could not unmarshal traversal proto: %w", err)
		}
		if s.eventProcessor != nil {
			if err := s.eventProcessor(ctx, traversal.WorkflowInstanceId, traversal.ElementId, traversal.TrackingId, traversal.Vars, false); err != nil {
				return false, fmt.Errorf("could not process event: %w", err)
			}
		}
		return true, nil
	})
	return
}

func (s *NatsService) processTracking(ctx context.Context) {
	s.process(ctx, "WORKFLOW.>", "Tracking", s.track)
	return
}

func (s *NatsService) processCompletedJobs(ctx context.Context) {
	s.process(ctx, messages.WorkFlowJobCompleteAll, "JobCompleteConsumer", func(ctx context.Context, msg *nats.Msg) (bool, error) {
		var job model.WorkflowState
		if err := proto.Unmarshal(msg.Data, &job); err != nil {
			return false, err
		}
		if s.eventJobCompleteProcessor != nil {
			if err := s.eventJobCompleteProcessor(ctx, job.TrackingId, job.Vars); err != nil {
				return false, err
			}
		}
		return true, nil
	})
}

func (s *NatsService) process(ctx context.Context, subject string, durable string, fn func(ctx context.Context, msg *nats.Msg) (bool, error)) {
	for i := 0; i < s.concurrency; i++ {
		go func() {
			sub, err := s.js.PullSubscribe(subject, durable)
			if err != nil {
				s.log.Error("process pull subscribe error", zap.Error(err), zap.String("subject", subject))
				return
			}
			for {
				select {
				case <-s.closing:
					return
				default:
				}
				ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
				msg, err := sub.Fetch(1, nats.Context(ctx))
				if err != nil {
					if err == context.DeadlineExceeded {
						cancel()
						continue
					}
					// Log Error
					s.log.Error("message fetch error", zap.Error(err))
					cancel()
					continue
				}
				m := msg[0]
				if embargo := m.Header.Get("embargo"); embargo != "" && embargo != "0" {
					e, err := strconv.Atoi(embargo)
					if err != nil {
						s.log.Error("bad embargo value", zap.Error(err))
						cancel()
						continue
					}
					offset := time.Duration(int64(e) - time.Now().UnixNano())
					if offset > 0 {
						m.NakWithDelay(offset)
						continue
					}
				}
				executeCtx := context.Background()
				ack, err := fn(executeCtx, msg[0])
				if err != nil {
					s.log.Error("processing error", zap.Error(err))
				}
				if ack {
					if err := msg[0].Ack(); err != nil {
						s.log.Error("processing failed to ack", zap.Error(err))
					}
				} else {
					if err := msg[0].Nak(); err != nil {
						s.log.Error("processing failed to nak", zap.Error(err))
					}
				}

				cancel()
			}
		}()
	}
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
		if err := common.SaveObj(nil, kv, st.WorkflowInstanceId, st); err != nil {
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
	s.process(ctx, messages.WorkflowMessages, "Message", s.processMessage)
	return
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
		if err := common.UpdateObj(s.wfMsgSubs, workflowInstanceId, &model.WorkflowInstanceSubscribers{}, func(v *model.WorkflowInstanceSubscribers) (*model.WorkflowInstanceSubscribers, error) {
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

func cleanExpression(s string) string {
	s = strings.TrimSpace(s)
	if strings.HasPrefix(s, "=") {
		if len(s) > 2 && s[2] != '=' {
			s = "=" + s
		}
	}
	return s
}

func (s *NatsService) Shutdown() {
	close(s.closing)
}

func (s *NatsService) processWorkflowEvents(ctx context.Context) {
	s.process(ctx, messages.WorkflowInstanceAll, "WorkflowConsumer", func(ctx context.Context, msg *nats.Msg) (bool, error) {
		var job model.WorkflowState
		if err := proto.Unmarshal(msg.Data, &job); err != nil {
			return false, err
		}
		if msg.Subject == "WORKFLOW.State.Workflow.Complete" {
			s.DestroyWorkflowInstance(ctx, job.WorkflowInstanceId)
		}
		return true, nil
	})
}
