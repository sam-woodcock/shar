package services

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	errors2 "errors"
	"fmt"
	"github.com/antonmedv/expr"
	"github.com/crystal-construct/shar/internal/messages"
	"github.com/crystal-construct/shar/model"
	"github.com/crystal-construct/shar/server/errors"
	"github.com/crystal-construct/shar/server/errors/keys"
	"github.com/crystal-construct/shar/server/services/ctxutil"
	"github.com/crystal-construct/shar/server/vars"
	"github.com/nats-io/nats.go"
	"github.com/segmentio/ksuid"
	"go.uber.org/zap"
	"google.golang.org/protobuf/proto"
	"strings"
	"time"
)

type NatsConn interface {
	JetStream(opts ...nats.JSOpt) (nats.JetStreamContext, error)
	Subscribe(subject string, fn nats.MsgHandler) (*nats.Subscription, error)
}

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
	conn                      NatsConn
}

func (s *NatsService) AwaitMsg(ctx context.Context, name string, state *model.WorkflowState) error {
	id := ksuid.New().String()
	if err := SaveObj(s.wfMsgSub, id, state); err != nil {
		return err
	}
	if err := UpdateObj(s.wfMsgSubs, state.WorkflowInstanceId, &model.WorkflowInstanceSubscribers{}, func(v *model.WorkflowInstanceSubscribers) (*model.WorkflowInstanceSubscribers, error) {
		v.List = append(v.List, id)
		return v, nil
	}); err != nil {
		return err
	}
	return nil
}

func (s *NatsService) ListWorkflows() (chan *model.ListWorkflowResult, chan error) {
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

func NewNatsService(log *zap.Logger, conn NatsConn, storageType nats.StorageType, concurrency int) (*NatsService, error) {
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
	keys := make([]string, 0, len(kvs))
	for k, _ := range kvs {
		keys = append(keys, k)
	}
	if err := ensureBuckets(js, storageType, keys); err != nil {
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
	wfId := ksuid.New().String()
	b, err := json.Marshal(wf)
	if err != nil {
		return "", err
	}
	h := sha256.New()
	if _, err := h.Write(b); err != nil {
		return "", fmt.Errorf("could not marshal workflow: %s", wf.Name)
	}
	hash := h.Sum(nil)
	err = SaveObj(s.wf, wfId, wf)
	if err != nil {
		return "", fmt.Errorf("could not save workflow: %s", wf.Name)
	}
	for _, m := range wf.Messages {
		ks := ksuid.New()
		if _, err := Load(s.wfMessageID, m.Name); err == nil {
			continue
		}
		if err := Save(s.wfMessageID, m.Name, []byte(ks.String())); err != nil {
			return "", err
		}
		if err := Save(s.wfMessageName, ks.String(), []byte(m.Name)); err != nil {
			return "", err
		}
	}
	if err := UpdateObj(s.wfVersion, wf.Name, &model.WorkflowVersions{}, func(v *model.WorkflowVersions) (*model.WorkflowVersions, error) {
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
	}); err != nil {
		fmt.Errorf("could not update workflow version for: %s", wf.Name)
	}
	return wfId, nil
}

func (s *NatsService) GetWorkflow(ctx context.Context, workflowId string) (*model.Workflow, error) {
	wf := &model.Workflow{}
	if err := LoadObj(s.wf, workflowId, wf); err == nats.ErrKeyNotFound {
		return nil, errors.ErrWorkflowNotFound
	} else if err != nil {
		return nil, fmt.Errorf("failed to load workflow from KV: %w", err)
	}
	return wf, nil
}

func (s *NatsService) CreateWorkflowInstance(ctx context.Context, wfInstance *model.WorkflowInstance) (*model.WorkflowInstance, error) {
	wfiId := ksuid.New().String()
	wfInstance.WorkflowInstanceId = wfiId
	if err := SaveObj(s.wfInstance, wfiId, wfInstance); err != nil {
		return nil, fmt.Errorf("failed to save workflow instance object to KV: %w", err)
	}
	subs := &model.WorkflowInstanceSubscribers{List: []string{}}
	if err := SaveObj(s.wfMsgSubs, wfiId, subs); err != nil {
		return nil, fmt.Errorf("failed to save workflow instance object to KV: %w", err)
	}
	return wfInstance, nil
}

func (s *NatsService) GetWorkflowInstance(ctx context.Context, workflowInstanceId string) (*model.WorkflowInstance, error) {
	wfi := &model.WorkflowInstance{}
	if err := LoadObj(s.wfInstance, workflowInstanceId, wfi); err == nats.ErrKeyNotFound {
		return nil, errors.ErrWorkflowNotFound
	} else if err != nil {
		return nil, fmt.Errorf("failed to load workflow instance from KV: %w", err)
	}
	return wfi, nil
}

func (s *NatsService) DestroyWorkflowInstance(ctx context.Context, workflowInstanceId string) error {
	wfi := &model.WorkflowInstance{}
	if err := LoadObj(s.wfInstance, workflowInstanceId, wfi); err != nil {
		s.log.Warn("Could not fetch workflow instance",
			zap.String(keys.WorkflowInstanceID, workflowInstanceId),
		)
		return err
	}
	wf := &model.Workflow{}
	if wfi.WorkflowId != "" {
		if err := LoadObj(s.wf, wfi.WorkflowId, wf); err != nil {
			s.log.Warn("Could not fetch workflow definition",
				zap.String(keys.WorkflowInstanceID, wfi.WorkflowInstanceId),
				zap.String(keys.WorkflowID, wfi.WorkflowId),
				zap.String(keys.WorkflowName, wf.Name),
			)
		}
	}

	if wf.Name != "" {
		for _, msg := range wf.Messages {
			msgIdB, err := Load(s.wfMessageID, msg.Name)
			if err != nil {
				s.log.Warn("Could not fetch message id for",
					zap.String("msg.name", msg.Name),
				)
			}
			msgId := string(msgIdB)
			var toDelete map[string]struct{}
			subs := &model.WorkflowInstanceSubscribers{}
			if err := LoadObj(s.wfMsgSubs, msgId, subs); err != nil {
				s.log.Warn("Could not fetch message subscribers",
					zap.String(keys.WorkflowInstanceID, wfi.WorkflowInstanceId),
					zap.String(keys.WorkflowID, wfi.WorkflowId),
					zap.String(keys.WorkflowName, wf.Name),
				)
			}
			for _, subid := range subs.List {
				sub := &model.WorkflowState{}
				if err := LoadObj(s.wfMsgSub, subid, sub); err != nil {
					s.log.Warn("Could not fetch message subscriber",
						zap.String(keys.WorkflowInstanceID, wfi.WorkflowInstanceId),
						zap.String(keys.WorkflowID, wfi.WorkflowId),
						zap.String(keys.WorkflowName, wf.Name),
						zap.String("sub.id", subid),
					)
					continue
				}
				if sub.WorkflowInstanceId == workflowInstanceId {
					toDelete[subid] = struct{}{}
				}
			}
			UpdateObj(s.wfMsgSubs, msgId, &model.WorkflowInstanceSubscribers{}, func(subs *model.WorkflowInstanceSubscribers) (*model.WorkflowInstanceSubscribers, error) {
				for i := 0; i < len(subs.List); i++ {
					val := subs.List[i]
					if _, ok := toDelete[val]; ok {
						subs.List = remove(subs.List, val)
						i--
						delete(toDelete, val)
					}
					err := s.wfMsgSub.Delete(val)
					if err != nil {
						s.log.Warn("Could not delete instance subscriber",
							zap.String("inst.id", val),
						)
					}
				}
				return subs, nil
			})
		}
	}

	if err := s.wfInstance.Delete(workflowInstanceId); err != nil {
		return err
	}
	if err := s.wfTracking.Delete(workflowInstanceId); err != nil {
		return err
	}

	return nil
}

func (s *NatsService) GetLatestVersion(ctx context.Context, workflowName string) (string, error) {
	v := &model.WorkflowVersions{}
	if err := LoadObj(s.wfVersion, workflowName, v); err == nats.ErrKeyNotFound {
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
	if err := SaveObj(s.job, jobId.String(), job); err != nil {
		return "", fmt.Errorf("failed to save job to KV: %w", err)
	}
	return jobId.String(), nil
}

func (s *NatsService) GetJob(ctx context.Context, id string) (*model.WorkflowState, error) {
	job := &model.WorkflowState{}
	if err := LoadObj(s.job, id, job); err != nil {
		return nil, fmt.Errorf("failed to load job from KV: %w", err)
	}
	return job, nil
}

func (s *NatsService) ListWorkflowInstance(workflowName string) (chan *model.ListWorkflowInstanceResult, chan error) {
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

func (s *NatsService) GetWorkflowInstanceStatus(id string) (*model.WorkflowInstanceStatus, error) {
	v := &model.WorkflowState{}
	err := LoadObj(s.wfTracking, id, v)
	if err != nil {
		return nil, fmt.Errorf("failed to load workflow instance status from KV: %w", err)
	}
	return &model.WorkflowInstanceStatus{State: []*model.WorkflowState{v}}, nil
}

/*
func (s *NatsService) AwaitMsg(ctx context.Context, name string, state *model.WorkflowState) error {
	return UpdateObj(s.wfMsgSubs, name, &model.MsgWaiting{}, func(v proto.Message) (proto.Message, error) {
		mw := v.(*model.MsgWaiting)
		mw.List = append(mw.List, state)
		return mw, nil
	})
}
*/

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

func (s *NatsService) PublishJob(ctx context.Context, stateName string, el *model.Element, job *model.WorkflowState) error {
	return s.PublishWorkflowState(ctx, stateName, job)
}

func (s *NatsService) PublishWorkflowState(ctx context.Context, stateName string, state *model.WorkflowState) error {
	state.UnixTimeNano = time.Now().UnixNano()
	msg := nats.NewMsg(stateName)
	if b, err := proto.Marshal(state); err != nil {
		return err
	} else {
		msg.Data = b
	}
	ctxutil.LoadNATSHeaderFromContext(ctx, msg)
	if _, err := s.js.PublishMsg(msg); err != nil {
		return err
	}
	return nil
}

func (s *NatsService) PublishMessage(ctx context.Context, workflowInstanceID string, name string, key string) error {
	messageIDb, err := Load(s.wfMessageID, name)
	messageID := string(messageIDb)
	if err != nil {
		return err
	}
	sharMsg := &model.MessageInstance{
		MessageId:      messageID,
		CorrelationKey: key,
	}
	msg := nats.NewMsg(fmt.Sprintf(messages.WorkflowMessageFormat, workflowInstanceID, messageID))
	if b, err := proto.Marshal(sharMsg); err != nil {
		return err
	} else {
		msg.Data = b
	}
	ctxutil.LoadNATSHeaderFromContext(ctx, msg)
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
			if err := s.eventProcessor(ctx, traversal.WorkflowInstanceId, traversal.ElementId, traversal.TrackingId, traversal.Vars); err != nil {
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
					return
				}
				executeCtx := ctxutil.LoadContextFromNATSHeader(ctx, msg[0])
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
		if err := SaveObj(kv, st.WorkflowInstanceId, st); err != nil {
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

func (s *NatsService) Conn() NatsConn {
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
	messageName, err := Load(s.wfMessageName, instance.MessageId)
	if err != nil {
		return false, fmt.Errorf("failed to load message name for message id %s: %w", instance.MessageId, err)
	}
	subj := strings.Split(msg.Subject, ".")
	if len(subj) < 4 {
		return true, nil
	}
	workflowInstanceId := subj[2]

	subs := &model.WorkflowInstanceSubscribers{}
	if err := LoadObj(s.wfMsgSubs, workflowInstanceId, subs); err != nil {
		return true, nil
	}
	for _, i := range subs.List {

		sub := &model.WorkflowState{}
		if err := LoadObj(s.wfMsgSub, i, sub); err == nats.ErrKeyNotFound {
			continue
		} else if err != nil {
			return false, err
		}
		if sub.Condition != string(messageName) {
			continue
		}
		dv, err := vars.Decode(s.log, sub.Vars)
		if err != nil {
			return false, err
		}
		success, err := s.evaluate(ctx, dv, instance.CorrelationKey+cleanExpression(sub.Execute))
		if err != nil {
			return false, err
		}
		if !success {
			continue
		}
		if s.eventProcessor != nil {
			if err := s.eventProcessor(ctx, sub.WorkflowInstanceId, sub.ElementId, sub.TrackingId, sub.Vars); err != nil {
				return false, fmt.Errorf("could not process event: %w", err)
			}
		}
		if s.messageCompleteProcessor != nil {
			if err := s.messageCompleteProcessor(ctx, sub); err != nil {
				return false, err
			}
		}
		if err := UpdateObj(s.wfMsgSubs, workflowInstanceId, &model.WorkflowInstanceSubscribers{}, func(v *model.WorkflowInstanceSubscribers) (*model.WorkflowInstanceSubscribers, error) {
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

/*
func (s *NatsService) getMessagePending(messageName string) (*model.MsgWaiting, uint64, error) {
	val, err := q.wfMsgSubs.Get(messageName)
	if err != nil {
		return nil, 0, fmt.Errorf("failed to get awaiting instances: %w", err)
	}
	pending := &model.MsgWaiting{}
	if err := proto.Unmarshal(val.Value(), pending); err != nil {
		return nil, 0, fmt.Errorf("could not unmarshal message proto: %w", err)
	}
	return pending, val.Revision(), nil
}
*/
func (s *NatsService) evaluate(ctx context.Context, vars model.Vars, expresssion string) (bool, error) {
	program, err := expr.Compile(expresssion, expr.Env(vars))
	if err != nil {
		s.log.Error("expression compilation error", zap.Error(err), zap.String("expr", expresssion))
		return false, fmt.Errorf("failed to compile expression: %w", err)
	}
	res, err := expr.Run(program, vars)
	if err != nil {
		s.log.Error("expression evaluation error", zap.String("expr", expresssion), zap.Error(err))
		return false, fmt.Errorf("failed to evaluate expression: %w", err)
	}
	return res.(bool), nil
}

/*
func (s *NatsService) removePending(ctx context.Context, toDelete []string, key string, msgWaiting *model.MsgWaiting, rev uint64) error {
	for {
		removed := make([]*model.WorkflowState, 0, len(msgWaiting.List))
		for i := 0; i < len(msgWaiting.List); i++ {
			if stringInSlice(msgWaiting.List[i].TrackingId, toDelete) {
				continue
			}
			removed = append(removed, msgWaiting.List[i])
		}
		msgWaiting.List = removed
		b, err := proto.Marshal(msgWaiting)
		if err != nil {
			return err
		}
		_, err = q.wfMsgSubs.Update(key, b, rev)
		if strings.Index(err.Error(), "wrong last sequence") > -1 {
			v, err := q.wfMsgSubs.Get(key)
			if err == nats.ErrKeyNotFound {
				continue
			}
			if err != nil {
				return err
			}
			if err = proto.Unmarshal(v.Value(), msgWaiting); err != nil {
				return err
			}
		}
	}
}
*/

func (s *NatsService) Shutdown() {
	close(s.closing)
}
