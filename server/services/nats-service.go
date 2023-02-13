package services

import (
	"bytes"
	"context"
	errors2 "errors"
	"fmt"
	"github.com/nats-io/nats.go"
	"github.com/segmentio/ksuid"
	"gitlab.com/shar-workflow/shar/common"
	"gitlab.com/shar-workflow/shar/common/element"
	"gitlab.com/shar-workflow/shar/common/expression"
	"gitlab.com/shar-workflow/shar/common/header"
	"gitlab.com/shar-workflow/shar/common/logx"
	"gitlab.com/shar-workflow/shar/common/setup"
	"gitlab.com/shar-workflow/shar/common/subj"
	"gitlab.com/shar-workflow/shar/common/workflow"
	"gitlab.com/shar-workflow/shar/model"
	"gitlab.com/shar-workflow/shar/server/errors"
	"gitlab.com/shar-workflow/shar/server/errors/keys"
	"gitlab.com/shar-workflow/shar/server/messages"
	"gitlab.com/shar-workflow/shar/server/vars"
	"golang.org/x/exp/slog"
	"google.golang.org/protobuf/proto"
	"strconv"
	"strings"
	"sync"
	"time"
)

// NatsService contains the engine functions that communicate with NATS.
type NatsService struct {
	js                             nats.JetStreamContext
	txJS                           nats.JetStreamContext
	messageCompleteProcessor       MessageCompleteProcessorFunc
	eventProcessor                 EventProcessorFunc
	eventJobCompleteProcessor      CompleteJobProcessorFunc
	traversalFunc                  TraversalFunc
	launchFunc                     LaunchFunc
	messageProcessor               MessageProcessorFunc
	storageType                    nats.StorageType
	concurrency                    int
	closing                        chan struct{}
	wfMsgSubs                      nats.KeyValue
	wfMsgSub                       nats.KeyValue
	wfInstance                     nats.KeyValue
	wfMessageName                  nats.KeyValue
	wfProcessInstance              nats.KeyValue
	wfMessageID                    nats.KeyValue
	wfUserTasks                    nats.KeyValue
	wfVarState                     nats.KeyValue
	wf                             nats.KeyValue
	wfVersion                      nats.KeyValue
	wfTracking                     nats.KeyValue
	job                            nats.KeyValue
	ownerName                      nats.KeyValue
	ownerID                        nats.KeyValue
	wfClientTask                   nats.KeyValue
	conn                           common.NatsConn
	txConn                         common.NatsConn
	workflowStats                  *model.WorkflowStats
	statsMx                        sync.Mutex
	wfName                         nats.KeyValue
	publishTimeout                 time.Duration
	eventActivityCompleteProcessor CompleteActivityProcessorFunc
	allowOrphanServiceTasks        bool
	completeActivityFunc           CompleteActivityFunc
	abortFunc                      AbortFunc
}

// WorkflowStats obtains the running counts for the engine
func (s *NatsService) WorkflowStats() *model.WorkflowStats {
	s.statsMx.Lock()
	defer s.statsMx.Unlock()
	return s.workflowStats
}

// AwaitMsg sets up a message subscription to wait for a workflow message
func (s *NatsService) AwaitMsg(ctx context.Context, state *model.WorkflowState) error {
	id := ksuid.New().String()
	if err := common.SaveObj(ctx, s.wfMsgSub, id, state); err != nil {
		return fmt.Errorf("failed to save workflow message state during await message: %w", err)
	}
	if err := common.UpdateObj(ctx, s.wfMsgSubs, state.WorkflowInstanceId, &model.WorkflowInstanceSubscribers{}, func(v *model.WorkflowInstanceSubscribers) (*model.WorkflowInstanceSubscribers, error) {
		v.List = append(v.List, id)
		return v, nil
	}); err != nil {
		return fmt.Errorf("failed to update the workflow message subscriptions during await message: %w", err)
	}
	return nil
}

// ListWorkflows returns a list of all the workflows in SHAR.
func (s *NatsService) ListWorkflows(ctx context.Context) (chan *model.ListWorkflowResult, chan error) {
	res := make(chan *model.ListWorkflowResult, 100)
	errs := make(chan error, 1)
	ks, err := s.wfVersion.Keys()
	if errors2.Is(err, nats.ErrNoKeysFound) {
		ks = []string{}
	} else if err != nil {
		errs <- err
		return res, errs
	}
	go func() {
		for _, k := range ks {
			v := &model.WorkflowVersions{}
			err := common.LoadObj(ctx, s.wfVersion, k, v)
			if errors2.Is(err, nats.ErrNoKeysFound) {
				continue
			}
			if err != nil {
				errs <- err
			}
			res <- &model.ListWorkflowResult{
				Name:    k,
				Version: v.Version[len(v.Version)-1].Number,
			}

		}
		close(res)
	}()
	return res, errs
}

// NewNatsService creates a new instance of the NATS communication layer.
func NewNatsService(conn common.NatsConn, txConn common.NatsConn, storageType nats.StorageType, concurrency int, allowOrphanServiceTasks bool) (*NatsService, error) {
	if concurrency < 1 || concurrency > 200 {
		return nil, fmt.Errorf("invalid concurrency: %w", errors2.New("invalid concurrency set"))
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
		conn:                    conn,
		txConn:                  txConn,
		js:                      js,
		txJS:                    txJS,
		concurrency:             concurrency,
		storageType:             storageType,
		closing:                 make(chan struct{}),
		workflowStats:           &model.WorkflowStats{},
		publishTimeout:          time.Second * 30,
		allowOrphanServiceTasks: allowOrphanServiceTasks,
	}

	if err := setup.EnsureWorkflowStream(js, storageType); err != nil {
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
	kvs[messages.KvOwnerID] = &ms.ownerID
	kvs[messages.KvOwnerName] = &ms.ownerName
	kvs[messages.KvClientTaskID] = &ms.wfClientTask
	kvs[messages.KvVarState] = &ms.wfVarState
	kvs[messages.KvProcessInstance] = &ms.wfProcessInstance
	ks := make([]string, 0, len(kvs))
	for k := range kvs {
		ks = append(ks, k)
	}
	if err := common.EnsureBuckets(js, storageType, ks); err != nil {
		return nil, fmt.Errorf("failed to ensure the KV buckets: %w", err)
	}

	for k, v := range kvs {
		kv, err := js.KeyValue(k)
		if err != nil {
			return nil, fmt.Errorf("failed to open %s KV: %w", k, err)
		}
		*v = kv
	}

	return ms, nil
}

// StoreWorkflow stores a workflow definition and returns a unique ID
func (s *NatsService) StoreWorkflow(ctx context.Context, wf *model.Workflow) (string, error) {

	// Populate Metadata
	s.populateMetadata(wf)

	// get this workflow name if it has already been registered
	_, err := s.wfName.Get(wf.Name)
	if errors2.Is(err, nats.ErrKeyNotFound) {
		wfNameID := ksuid.New().String()
		_, err = s.wfName.Put(wf.Name, []byte(wfNameID))
		if err != nil {
			return "", fmt.Errorf("failed to store the workflow id during store workflow: %w", err)
		}
	} else if err != nil {
		return "", fmt.Errorf("failed to get an existing workflow id: %w", err)
	}

	wfID := ksuid.New().String()
	hash, err2 := workflow.GetHash(wf)
	if err2 != nil {
		return "", fmt.Errorf("store workflow failed to get the workflow hash: %w", err2)
	}

	var newWf bool
	if err := common.UpdateObj(ctx, s.wfVersion, wf.Name, &model.WorkflowVersions{}, func(v *model.WorkflowVersions) (*model.WorkflowVersions, error) {
		n := len(v.Version)
		if v.Version == nil || n == 0 {
			v.Version = make([]*model.WorkflowVersion, 0, 1)
		} else {
			if bytes.Equal(hash, v.Version[n-1].Sha256) {
				wfID = v.Version[n-1].Id
				return v, nil
			}
		}
		newWf = true
		err = common.SaveObj(ctx, s.wf, wfID, wf)
		if err != nil {
			return nil, fmt.Errorf("could not save workflow: %s", wf.Name)
		}
		v.Version = append(v.Version, &model.WorkflowVersion{Id: wfID, Sha256: hash, Number: int32(n) + 1})
		return v, nil
	}); err != nil {
		return "", fmt.Errorf("could not update workflow version for: %s", wf.Name)
	}

	if !newWf {
		return wfID, nil
	}

	for _, m := range wf.Messages {
		ks := ksuid.New()
		if _, err := common.Load(ctx, s.wfMessageID, m.Name); err == nil {
			continue
		}
		if err := common.Save(ctx, s.wfMessageID, m.Name, []byte(ks.String())); err != nil {
			return "", fmt.Errorf("failed to save a message name during workflow creation: %w", err)
		}
		if err := common.Save(ctx, s.wfMessageName, ks.String(), []byte(m.Name)); err != nil {
			return "", fmt.Errorf("failed to save a message id during workflow creation: %w", err)
		}
		if err := common.Save(ctx, s.wfClientTask, wf.Name+"_"+m.Name, []byte(ks.String())); err != nil {
			return "", fmt.Errorf("failed to create a client task during workflow creation: %w", err)
		}

		jxCfg := &nats.ConsumerConfig{
			Durable:       "ServiceTask_" + ks.String(),
			Description:   "",
			FilterSubject: subj.NS(messages.WorkflowJobSendMessageExecute, "default") + "." + ks.String(),
			AckPolicy:     nats.AckExplicitPolicy,
			MaxAckPending: 65536,
		}
		if err = ensureConsumer(s.js, "WORKFLOW", jxCfg); err != nil {
			return "", fmt.Errorf("failed to add service task consumer: %w", err)
		}

	}
	for _, i := range wf.Process {
		for _, j := range i.Elements {
			if j.Type == element.ServiceTask {
				id := ksuid.New().String()
				_, err := s.wfClientTask.Get(j.Execute)
				if err != nil && errors2.Is(err, nats.ErrKeyNotFound) {
					_, err := s.wfClientTask.Put(j.Execute, []byte(id))
					if err != nil {
						return "", fmt.Errorf("failed to add task to registry: %w", err)
					}

					jxCfg := &nats.ConsumerConfig{
						Durable:       "ServiceTask_" + id,
						Description:   "",
						FilterSubject: subj.NS(messages.WorkflowJobServiceTaskExecute, "default") + "." + id,
						AckPolicy:     nats.AckExplicitPolicy,
					}

					if err = ensureConsumer(s.js, "WORKFLOW", jxCfg); err != nil {
						return "", fmt.Errorf("failed to add service task consumer: %w", err)
					}
				}
			}
		}
	}

	go s.incrementWorkflowCount()

	return wfID, nil
}

func ensureConsumer(js nats.JetStreamContext, streamName string, consumerConfig *nats.ConsumerConfig) error {
	if _, err := js.ConsumerInfo(streamName, consumerConfig.Durable); errors2.Is(err, nats.ErrConsumerNotFound) {
		if _, err := js.AddConsumer(streamName, consumerConfig); err != nil {
			panic(err)
		}
	} else if err != nil {
		return fmt.Errorf("failed during call to get consumer info in ensure consumer: %w", err)
	}
	return nil
}

// GetWorkflow - retrieves a workflow model given its ID
func (s *NatsService) GetWorkflow(ctx context.Context, workflowID string) (*model.Workflow, error) {
	wf := &model.Workflow{}
	if err := common.LoadObj(ctx, s.wf, workflowID, wf); errors2.Is(err, nats.ErrKeyNotFound) {
		return nil, fmt.Errorf("get workflow failed to load object: %w", errors.ErrWorkflowNotFound)

	} else if err != nil {
		return nil, fmt.Errorf("failed to load workflow from KV: %w", err)
	}
	return wf, nil
}

// GetWorkflowVersions - returns a list of versions for a given workflow.
func (s *NatsService) GetWorkflowVersions(ctx context.Context, workflowName string) (*model.WorkflowVersions, error) {
	ver := &model.WorkflowVersions{}
	if err := common.LoadObj(ctx, s.wfVersion, workflowName, ver); errors2.Is(err, nats.ErrKeyNotFound) {
		return nil, fmt.Errorf("get workflow versions failed to load object: %w", errors.ErrWorkflowVersionNotFound)

	} else if err != nil {
		return nil, fmt.Errorf("failed to load workflow from KV: %w", err)
	}
	return ver, nil
}

// CreateWorkflowInstance given a workflow, starts a new workflow instance and returns its ID
func (s *NatsService) CreateWorkflowInstance(ctx context.Context, wfInstance *model.WorkflowInstance) (*model.WorkflowInstance, error) {
	wfiID := ksuid.New().String()
	log := slog.FromContext(ctx)
	log.Info("creating workflow instance", slog.String(keys.WorkflowInstanceID, wfiID))
	wfInstance.WorkflowInstanceId = wfiID
	wfInstance.ProcessInstanceId = []string{}
	wfInstance.SatisfiedProcesses = map[string]bool{".": true}
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

// GetWorkflowInstance retrieves workflow instance given its ID.
func (s *NatsService) GetWorkflowInstance(ctx context.Context, workflowInstanceID string) (*model.WorkflowInstance, error) {
	wfi := &model.WorkflowInstance{}
	if err := common.LoadObj(ctx, s.wfInstance, workflowInstanceID, wfi); errors2.Is(err, nats.ErrKeyNotFound) {
		return nil, fmt.Errorf("get workflow instance failed to load object: %w", errors.ErrWorkflowInstanceNotFound)
	} else if err != nil {
		return nil, fmt.Errorf("failed to load workflow instance from KV: %w", err)
	}
	return wfi, nil
}

// GetServiceTaskRoutingKey gets a unique ID for a service task that can be used to listen for its activation.
func (s *NatsService) GetServiceTaskRoutingKey(ctx context.Context, taskName string) (string, error) {
	var b []byte
	var err error
	if b, err = common.Load(ctx, s.wfClientTask, taskName); err != nil && errors2.Is(err, nats.ErrKeyNotFound) {
		if !s.allowOrphanServiceTasks {
			return "", fmt.Errorf("failed attempt to get service task key. key not present: %w", err)
		}
		id := ksuid.New().String()
		_, err := s.wfClientTask.Put(taskName, []byte(id))
		if err != nil {
			return "", fmt.Errorf("failed to register service task key: %w", err)
		}
		return id, nil
	} else if err != nil {
		return "", fmt.Errorf("failed attempt to get service task key: %w", err)
	}
	return string(b), nil
}

// GetMessageSenderRoutingKey gets an ID used to listen for workflow message instances.
func (s *NatsService) GetMessageSenderRoutingKey(ctx context.Context, workflowName string, messageName string) (string, error) {
	_, err := s.wfName.Get(workflowName)
	if err != nil {
		return "", fmt.Errorf("cannot locate workflow: %w", err)
	}
	var b []byte
	if b, err = common.Load(ctx, s.wfClientTask, workflowName+"_"+messageName); err != nil {
		return "", fmt.Errorf("failed attempt to get service task key: %w", err)
	}
	return string(b), nil
}

// XDestroyWorkflowInstance terminates a running workflow instance with a cancellation reason and error
func (s *NatsService) XDestroyWorkflowInstance(ctx context.Context, state *model.WorkflowState, cancellationState model.CancellationState, wfError *model.Error) error {
	log := slog.FromContext(ctx)
	log.Info("destroying workflow instance", slog.String(keys.WorkflowInstanceID, state.WorkflowInstanceId))
	// Get the workflow instance
	wfi := &model.WorkflowInstance{}
	if err := common.LoadObj(ctx, s.wfInstance, state.WorkflowInstanceId, wfi); err != nil {
		log.Warn("Could not fetch workflow instance",
			slog.String(keys.WorkflowInstanceID, state.WorkflowInstanceId),
		)
		return s.expectPossibleMissingKey(ctx, "error fetching workflow instance", err)
	}
	// TODO: soft error
	for _, piID := range wfi.ProcessInstanceId {
		pi, err := s.GetProcessInstance(ctx, piID)
		if err != nil {
			return err
		}
		err = s.DestroyProcessInstance(ctx, state, pi, wfi)
		if err != nil {
			return err
		}
	}
	// Get the workflow
	wf := &model.Workflow{}
	if wfi.WorkflowId != "" {
		if err := common.LoadObj(ctx, s.wf, wfi.WorkflowId, wf); err != nil {
			log.Warn("Could not fetch workflow definition",
				slog.String(keys.WorkflowInstanceID, wfi.WorkflowInstanceId),
				slog.String(keys.WorkflowID, wfi.WorkflowId),
				slog.String(keys.WorkflowName, wf.Name),
			)
		}
	}
	// Get all the subscriptions
	subs := &model.WorkflowInstanceSubscribers{}
	if err := common.LoadObj(ctx, s.wfMsgSubs, wfi.WorkflowInstanceId, subs); err != nil {
		log.Debug("Could not fetch message subscribers",
			slog.String(keys.WorkflowInstanceID, wfi.WorkflowInstanceId),
			slog.String(keys.WorkflowID, wfi.WorkflowId),
			slog.String(keys.WorkflowName, wf.Name),
			slog.String(keys.MessageID, wf.Name),
		)
	}

	if err := common.UpdateObj(ctx, s.wfMsgSubs, state.WorkflowInstanceId, &model.WorkflowInstanceSubscribers{}, func(subs *model.WorkflowInstanceSubscribers) (*model.WorkflowInstanceSubscribers, error) {
		for i := 0; i < len(subs.List); i++ {
			err := s.wfMsgSub.Delete(subs.List[i])
			if err != nil {
				log.Debug("could not delete instance subscriber",
					slog.String("inst.id", subs.List[i]),
				)
			}
		}
		return subs, nil
	}); err != nil {
		return s.expectPossibleMissingKey(ctx, "could not update message subscriptions", err)
	}

	tState := &model.WorkflowState{
		WorkflowId:         wf.Name,
		WorkflowInstanceId: wfi.WorkflowInstanceId,
		State:              cancellationState,
		Error:              wfError,
		UnixTimeNano:       time.Now().UnixNano(),
		WorkflowName:       wf.Name,
	}

	if tState.Error != nil {
		tState.State = model.CancellationState_errored
	}

	if err := s.deleteWorkflowInstance(ctx, tState); err != nil {
		return fmt.Errorf("failed to delete workflow state whilst destroying workflow instance: %w", err)
	}
	s.incrementWorkflowCompleted()
	return nil
}

func (s *NatsService) deleteWorkflowInstance(ctx context.Context, state *model.WorkflowState) error {
	if err := s.wfInstance.Delete(state.WorkflowInstanceId); err != nil && !errors2.Is(err, nats.ErrKeyNotFound) {
		return fmt.Errorf("could not delete workflow instance: %w", err)
	}
	if err := s.wfMsgSubs.Delete(state.WorkflowInstanceId); err != nil && !errors2.Is(err, nats.ErrKeyNotFound) {
		return fmt.Errorf("could not delete message subscriptions: %w", err)
	}

	if err := s.wfTracking.Delete(state.WorkflowInstanceId); err != nil && !errors2.Is(err, nats.ErrKeyNotFound) {
		return fmt.Errorf("could not delete workflow tracking: %w", err)
	}
	if err := s.PublishWorkflowState(ctx, messages.WorkflowInstanceTerminated, state); err != nil {
		return fmt.Errorf("could not send workflow terminate message: %w", err)
	}
	return nil
}

// GetLatestVersion queries the workflow versions table for the latest entry
func (s *NatsService) GetLatestVersion(ctx context.Context, workflowName string) (string, error) {
	v := &model.WorkflowVersions{}
	if err := common.LoadObj(ctx, s.wfVersion, workflowName, v); errors2.Is(err, nats.ErrKeyNotFound) {
		return "", fmt.Errorf("failed to get latest workflow version: %w", errors.ErrWorkflowNotFound)
	} else if err != nil {
		return "", fmt.Errorf("failed load object whist getting latest versiony: %w", err)
	} else {
		return v.Version[len(v.Version)-1].Id, nil
	}
}

// CreateJob stores a workflow task state.
func (s *NatsService) CreateJob(ctx context.Context, job *model.WorkflowState) (string, error) {
	tid := ksuid.New().String()
	job.Id = common.TrackingID(job.Id).Push(tid)
	if err := common.SaveObj(ctx, s.job, tid, job); err != nil {
		return "", fmt.Errorf("failed to save job to KV: %w", err)
	}
	return tid, nil
}

// GetJob gets a workflow task state.
func (s *NatsService) GetJob(ctx context.Context, trackingID string) (*model.WorkflowState, error) {
	job := &model.WorkflowState{}
	if err := common.LoadObj(ctx, s.job, trackingID, job); err == nil {
		return job, nil
	} else if errors2.Is(err, nats.ErrKeyNotFound) {
		return nil, fmt.Errorf("get job failed to load workflow object: %w", errors.ErrJobNotFound)
	} else if err != nil {
		return nil, fmt.Errorf("failed to load job from KV: %w", err)
	} else {
		return job, nil
	}
}

// DeleteJob removes a workflow task state.
func (s *NatsService) DeleteJob(_ context.Context, trackingID string) error {
	if err := common.Delete(s.job, trackingID); err != nil {
		return fmt.Errorf("failed attempt to delete job: %w", err)
	}
	return nil
}

// ListWorkflowInstance returns a list of running workflows and versions given a workflow ID
func (s *NatsService) ListWorkflowInstance(ctx context.Context, workflowName string) (chan *model.ListWorkflowInstanceResult, chan error) {
	log := slog.FromContext(ctx)
	errs := make(chan error, 1)
	wch := make(chan *model.ListWorkflowInstanceResult, 100)

	wfv := &model.WorkflowVersions{}
	if err := common.LoadObj(ctx, s.wfVersion, workflowName, wfv); err != nil {
		errs <- err
		return wch, errs
	}

	ver := make(map[string]*model.WorkflowVersion)
	for _, v := range wfv.Version {
		ver[v.Id] = v
	}

	ks, err := s.wfInstance.Keys()
	if errors2.Is(err, nats.ErrNoKeysFound) {
		ks = []string{}
	} else if err != nil {
		log := slog.FromContext(ctx)
		log.Error("error obtaining keys", err)
		return nil, errs
	}
	go func(keys []string) {
		for _, k := range keys {
			v := &model.WorkflowInstance{}
			err := common.LoadObj(ctx, s.wfInstance, k, v)
			if wv, ok := ver[v.WorkflowId]; ok {
				if err != nil && err != nats.ErrKeyNotFound {
					errs <- err
					log.Error("error loading object", err)
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

// ListWorkflowInstanceProcesses gets the current processIDs for a workflow instance.
func (s *NatsService) ListWorkflowInstanceProcesses(ctx context.Context, id string) ([]string, error) {
	v := &model.WorkflowInstance{}
	err := common.LoadObj(ctx, s.wfInstance, id, v)
	if err != nil {
		return nil, fmt.Errorf("failed to load workflow instance from KV: %w", err)
	}
	return v.ProcessInstanceId, nil
}

// GetProcessInstanceStatus returns a list of workflow statuses for the specified process instance ID.
func (s *NatsService) GetProcessInstanceStatus(ctx context.Context, id string) ([]*model.WorkflowState, error) {
	v := &model.WorkflowState{}
	err := common.LoadObj(ctx, s.wfTracking, id, v)
	if err != nil {
		return nil, fmt.Errorf("function GetProcessInstanceStatus failed to load from KV: %w", err)
	}
	return []*model.WorkflowState{v}, nil
}

// StartProcessing begins listening to all the message processing queues.
func (s *NatsService) StartProcessing(ctx context.Context) error {

	if err := s.processTraversals(ctx); err != nil {
		return fmt.Errorf("failed to start traversals handler: %w", err)
	}
	if err := s.processJobAbort(ctx); err != nil {
		return fmt.Errorf("failed to start job abort handler: %w", err)
	}
	if err := s.processGeneralAbort(ctx); err != nil {
		return fmt.Errorf("failed to general abort handler: %w", err)
	}
	if err := s.processTracking(ctx); err != nil {
		return fmt.Errorf("failed to start tracking handler: %w", err)
	}
	if err := s.processWorkflowEvents(ctx); err != nil {
		return fmt.Errorf("failed to start workflow events handler: %w", err)
	}
	if err := s.processMessages(ctx); err != nil {
		return fmt.Errorf("failed to start process messages handler: %w", err)
	}
	if err := s.listenForTimer(ctx, s.js, s.closing, 4); err != nil {
		return fmt.Errorf("failed to start timer handler: %w", err)
	}
	if err := s.processCompletedJobs(ctx); err != nil {
		return fmt.Errorf("failed to start completed jobs handler: %w", err)
	}
	if err := s.processActivities(ctx); err != nil {
		return fmt.Errorf("failed to start activities handler: %w", err)
	}
	if err := s.processLaunch(ctx); err != nil {
		return fmt.Errorf("failed to start launch handler: %w", err)
	}
	if err := s.processProcessComplete(ctx); err != nil {
		return fmt.Errorf("failed to start process complete handler: %w", err)
	}

	return nil
}

// SetEventProcessor sets the callback for processing workflow activities.
func (s *NatsService) SetEventProcessor(processor EventProcessorFunc) {
	s.eventProcessor = processor
}

// SetMessageCompleteProcessor sets the callback for completed messages.
func (s *NatsService) SetMessageCompleteProcessor(processor MessageCompleteProcessorFunc) {
	s.messageCompleteProcessor = processor
}

// SetMessageProcessor sets the callback used to create new workflow instances based on a timer.
func (s *NatsService) SetMessageProcessor(processor MessageProcessorFunc) {
	s.messageProcessor = processor
}

// SetCompleteJobProcessor sets the callback for completed tasks.
func (s *NatsService) SetCompleteJobProcessor(processor CompleteJobProcessorFunc) {
	s.eventJobCompleteProcessor = processor
}

// SetCompleteActivityProcessor sets the callback fired when an activity completes.
func (s *NatsService) SetCompleteActivityProcessor(processor CompleteActivityProcessorFunc) {
	s.eventActivityCompleteProcessor = processor
}

// SetLaunchFunc sets the callback used to start child workflows.
func (s *NatsService) SetLaunchFunc(processor LaunchFunc) {
	s.launchFunc = processor
}

// SetTraversalProvider sets the callback used to handle traversals.
func (s *NatsService) SetTraversalProvider(provider TraversalFunc) {
	s.traversalFunc = provider
}

// SetCompleteActivity sets the callback which generates complete activity events.
func (s *NatsService) SetCompleteActivity(processor CompleteActivityFunc) {
	s.completeActivityFunc = processor
}

// SetAbort sets the function called when a workflow object aborts.
func (s *NatsService) SetAbort(processor AbortFunc) {
	s.abortFunc = processor
}

// PublishWorkflowState publishes a SHAR state object to a given subject
func (s *NatsService) PublishWorkflowState(ctx context.Context, stateName string, state *model.WorkflowState, opts ...PublishOpt) error {
	c := &publishOptions{}
	for _, i := range opts {
		i.Apply(c)
	}
	state.UnixTimeNano = time.Now().UnixNano()
	msg := nats.NewMsg(subj.NS(stateName, "default"))
	msg.Header.Set("embargo", strconv.Itoa(c.Embargo))
	b, err := proto.Marshal(state)
	if err != nil {
		return fmt.Errorf("failed to marshal proto during publish workflow state: %w", err)
	}
	msg.Data = b
	if err := header.FromCtxToMsgHeader(ctx, &msg.Header); err != nil {
		return fmt.Errorf("failed to add header to published workflow state: %w", err)
	}
	pubCtx, cancel := context.WithTimeout(ctx, s.publishTimeout)
	defer cancel()
	if c.ID == "" {
		c.ID = ksuid.New().String()
	}

	if _, err := s.txJS.PublishMsg(msg, nats.Context(pubCtx), nats.MsgId(c.ID)); err != nil {
		log := slog.FromContext(ctx)
		log.Error("failed to publish message", err, slog.String("nats.msg.id", c.ID), slog.Any("state", state), slog.String("subject", msg.Subject))
		return fmt.Errorf("failed to publish workflow state message: %w", err)
	}
	if stateName == subj.NS(messages.WorkflowJobUserTaskExecute, "default") {
		for _, i := range append(state.Owners, state.Groups...) {
			if err := s.openUserTask(ctx, i, common.TrackingID(state.Id).ID()); err != nil {
				return fmt.Errorf("failed to open user task during publish workflow state: %w", err)
			}
		}
	}
	return nil
}

// PublishMessage publishes a workflow message.
func (s *NatsService) PublishMessage(ctx context.Context, workflowInstanceID string, name string, key string, vars []byte) error {
	messageIDb, err := common.Load(ctx, s.wfMessageID, name)
	messageID := string(messageIDb)
	if err != nil {
		return fmt.Errorf("failed to resolve message id: %w", err)
	}
	sharMsg := &model.MessageInstance{
		MessageId:      messageID,
		CorrelationKey: key,
		Vars:           vars,
	}
	msg := nats.NewMsg(fmt.Sprintf(messages.WorkflowMessageFormat, "default", workflowInstanceID, messageID))
	b, err := proto.Marshal(sharMsg)
	if err != nil {
		return fmt.Errorf("failed to marshal message for publishing: %w", err)
	}
	msg.Data = b
	if err := header.FromCtxToMsgHeader(ctx, &msg.Header); err != nil {
		return fmt.Errorf("failed to add header to published workflow state: %w", err)
	}
	pubCtx, cancel := context.WithTimeout(ctx, s.publishTimeout)
	defer cancel()
	id := ksuid.New().String()
	if _, err := s.txJS.PublishMsg(msg, nats.Context(pubCtx), nats.MsgId(id)); err != nil {
		log := slog.FromContext(ctx)
		log.Error("failed to publish message", err, slog.String("nats.msg.id", id), slog.Any("msg", sharMsg), slog.String("subject", msg.Subject))
		return fmt.Errorf("failed to publish message: %w", err)
	}
	return nil
}

// GetElement gets the definition for the current element given a workflow state.
func (s *NatsService) GetElement(ctx context.Context, state *model.WorkflowState) (*model.Element, error) {
	wf := &model.Workflow{}
	if err := common.LoadObj(ctx, s.wf, state.WorkflowId, wf); errors2.Is(err, nats.ErrKeyNotFound) {
		return nil, fmt.Errorf("failed load object during get element: %w", err)
	}
	els := common.ElementTable(wf)
	if el, ok := els[state.ElementId]; ok {
		return el, nil
	}
	return nil, fmt.Errorf("get element failed to locate %s: %w", state.ElementId, errors.ErrElementNotFound)
}

func (s *NatsService) processTraversals(ctx context.Context) error {
	err := common.Process(ctx, s.js, "traversal", s.closing, subj.NS(messages.WorkflowTraversalExecute, "*"), "Traversal", s.concurrency, func(ctx context.Context, log *slog.Logger, msg *nats.Msg) (bool, error) {
		var traversal model.WorkflowState
		if err := proto.Unmarshal(msg.Data, &traversal); err != nil {
			return false, fmt.Errorf("could not unmarshal traversal proto: %w", err)
		}

		if _, _, err := s.HasValidProcess(ctx, traversal.ProcessInstanceId, traversal.WorkflowInstanceId); errors2.Is(err, errors.ErrWorkflowInstanceNotFound) || errors2.Is(err, errors.ErrProcessInstanceNotFound) {
			log := slog.FromContext(ctx)
			log.Log(slog.InfoLevel, "processTraversals aborted due to a missing process")
			return true, nil
		} else if err != nil {
			return false, err
		}

		if s.eventProcessor != nil {
			activityID := ksuid.New().String()
			if err := s.SaveState(ctx, activityID, &traversal); err != nil {
				return false, err
			}
			if err := s.eventProcessor(ctx, activityID, &traversal, false); errors.IsWorkflowFatal(err) {
				slog.FromContext(ctx).Error("workflow fatally terminated whilst processing activity", err, slog.String(keys.WorkflowInstanceID, traversal.WorkflowInstanceId), slog.String(keys.WorkflowID, traversal.WorkflowId), err, slog.String(keys.ElementID, traversal.ElementId))
				return true, nil
			} else if err != nil {
				return false, fmt.Errorf("could not process event: %w", err)
			}
		}
		return true, nil
	})
	if err != nil {
		return fmt.Errorf("failed during traversal processor: %w", err)
	}
	return nil
}

// HasValidProcess - checks for a valid process and instance for a workflow process and instance ids
func (s *NatsService) HasValidProcess(ctx context.Context, processInstanceId, workflowInstanceId string) (*model.ProcessInstance, *model.WorkflowInstance, error) {
	wfi, err := s.hasValidInstance(ctx, workflowInstanceId)
	if err != nil {
		return nil, nil, err
	}
	pi, err := s.GetProcessInstance(ctx, processInstanceId)
	if errors2.Is(err, errors.ErrProcessInstanceNotFound) {
		return nil, nil, fmt.Errorf("orphaned activity: %w", err)
	}
	if err != nil {
		return nil, nil, err
	}
	return pi, wfi, err
}

func (s *NatsService) hasValidInstance(ctx context.Context, workflowInstanceId string) (*model.WorkflowInstance, error) {
	wfi, err := s.GetWorkflowInstance(ctx, workflowInstanceId)
	if errors2.Is(err, errors.ErrWorkflowInstanceNotFound) {
		return nil, fmt.Errorf("orphaned activity: %w", err)
	}
	if err != nil {
		return nil, err
	}
	return wfi, err
}

func (s *NatsService) processTracking(ctx context.Context) error {
	err := common.Process(ctx, s.js, "tracking", s.closing, "WORKFLOW.>", "Tracking", 1, s.track)
	if err != nil {
		return fmt.Errorf("failed during tracking processor: %w", err)
	}
	return nil
}

func (s *NatsService) processCompletedJobs(ctx context.Context) error {
	err := common.Process(ctx, s.js, "completedJob", s.closing, subj.NS(messages.WorkFlowJobCompleteAll, "*"), "JobCompleteConsumer", s.concurrency, func(ctx context.Context, log *slog.Logger, msg *nats.Msg) (bool, error) {
		var job model.WorkflowState
		if err := proto.Unmarshal(msg.Data, &job); err != nil {
			return false, fmt.Errorf("failed to unmarshal completed job state: %w", err)
		}
		if _, _, err := s.HasValidProcess(ctx, job.ProcessInstanceId, job.WorkflowInstanceId); errors2.Is(err, errors.ErrWorkflowInstanceNotFound) || errors2.Is(err, errors.ErrProcessInstanceNotFound) {
			log := slog.FromContext(ctx)
			log.Log(slog.InfoLevel, "processCompletedJobs aborted due to a missing process")
			return true, nil
		} else if err != nil {
			return false, err
		}
		if s.eventJobCompleteProcessor != nil {
			if err := s.eventJobCompleteProcessor(ctx, &job); err != nil {
				return false, err
			}
		}
		return true, nil
	})
	if err != nil {
		return fmt.Errorf("failed during completed job processor: %w", err)
	}
	return nil
}

func (s *NatsService) track(ctx context.Context, log *slog.Logger, msg *nats.Msg) (bool, error) {
	sj := msg.Subject
	switch {
	case strings.HasSuffix(sj, ".State.Workflow.Execute"),
		strings.HasSuffix(sj, ".State.Process.Execute"),
		strings.HasSuffix(sj, ".State.Traversal.Execute"),
		strings.HasSuffix(sj, ".State.Activity.Execute"),
		strings.Contains(sj, ".State.Job.Execute."):
		st := &model.WorkflowState{}
		if err := proto.Unmarshal(msg.Data, st); err != nil {
			return false, fmt.Errorf("unmarshal failed during tracking 'execute' event: %w", err)
		}
		if err := common.SaveObj(ctx, s.wfTracking, st.WorkflowInstanceId, st); err != nil {
			return false, fmt.Errorf("failed to save tracking information: %w", err)
		}
	case strings.HasSuffix(sj, ".State.Workflow.Complete"),
		strings.HasSuffix(sj, ".State.Process.Complete"),
		strings.HasSuffix(sj, ".State.Traversal.Complete"),
		strings.HasSuffix(sj, ".State.Activity.Complete"),
		strings.Contains(sj, ".State.Job.Complete."):
		st := &model.WorkflowState{}
		if err := proto.Unmarshal(msg.Data, st); err != nil {
			return false, fmt.Errorf("unmarshall failed during tracking 'complete' event: %w", err)
		}
		if err := s.wfTracking.Delete(st.WorkflowInstanceId); err != nil {
			return false, fmt.Errorf("failed to delete workflow instance upon completion: %w", err)
		}
	default:

	}
	return true, nil
}

// Conn returns the active nats connection
func (s *NatsService) Conn() common.NatsConn { //nolint:ireturn
	return s.conn
}

func (s *NatsService) processMessages(ctx context.Context) error {
	err := common.Process(ctx, s.js, "message", s.closing, subj.NS(messages.WorkflowMessages, "*"), "Message", s.concurrency, s.processMessage)
	if err != nil {
		return fmt.Errorf("failed to start message processor: %w", err)
	}
	return nil
}

func (s *NatsService) processMessage(ctx context.Context, log *slog.Logger, msg *nats.Msg) (bool, error) {
	// Unpack the message Instance
	instance := &model.MessageInstance{}
	if err := proto.Unmarshal(msg.Data, instance); err != nil {
		return false, fmt.Errorf("could not unmarshal message proto: %w", err)
	}
	messageName, err := common.Load(ctx, s.wfMessageName, instance.MessageId)
	if err != nil {
		return false, fmt.Errorf("failed to load message name for message id %s: %w", instance.MessageId, err)
	}
	sj := strings.Split(msg.Subject, ".")
	if len(sj) < 4 {
		return true, nil
	}
	workflowInstanceID := sj[3]
	subs := &model.WorkflowInstanceSubscribers{}
	if err := common.LoadObj(ctx, s.wfMsgSubs, workflowInstanceID, subs); err != nil {
		return true, nil
	}
	for _, i := range subs.List {

		sub := &model.WorkflowState{}
		if err := common.LoadObj(ctx, s.wfMsgSub, i, sub); errors2.Is(err, nats.ErrKeyNotFound) {
			continue
		} else if err != nil {
			return false, fmt.Errorf("failed to load workflow state processing message subscribers list: %w", err)
		}
		if *sub.Condition != string(messageName) {
			continue
		}

		dv, err := vars.Decode(ctx, sub.Vars)
		if err != nil {
			return false, fmt.Errorf("failed to decode message subscription variables: %w", err)
		}
		success, err := expression.Eval[bool](ctx, *sub.Execute+"=="+instance.CorrelationKey, dv)
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

		err = vars.OutputVars(ctx, instance.Vars, &sub.Vars, el.OutputTransform)
		if err != nil {
			return false, fmt.Errorf("failed to transform output variables for message: %w", err)
		}

		if s.messageCompleteProcessor != nil {
			if err := s.messageCompleteProcessor(ctx, sub); err != nil {
				return false, err
			}
		}
		if err := common.UpdateObj(ctx, s.wfMsgSubs, workflowInstanceID, &model.WorkflowInstanceSubscribers{}, func(v *model.WorkflowInstanceSubscribers) (*model.WorkflowInstanceSubscribers, error) {
			remove(v.List, i)
			return v, nil
		}); err != nil {
			return false, fmt.Errorf("failed to update message subscriptions: %w", err)
		}
		// TODO: Should we close something here?
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

// Shutdown signals the engine to stop processing.
func (s *NatsService) Shutdown() {
	close(s.closing)
}

func (s *NatsService) processWorkflowEvents(ctx context.Context) error {
	err := common.Process(ctx, s.js, "workflowEvent", s.closing, subj.NS(messages.WorkflowInstanceAll, "*"), "WorkflowConsumer", s.concurrency, func(ctx context.Context, log *slog.Logger, msg *nats.Msg) (bool, error) {
		var job model.WorkflowState
		if err := proto.Unmarshal(msg.Data, &job); err != nil {
			return false, fmt.Errorf("failed to load workflow state processing workflow event: %w", err)
		}
		if strings.HasSuffix(msg.Subject, ".State.Workflow.Complete") {
			if _, err := s.hasValidInstance(ctx, job.WorkflowInstanceId); errors2.Is(err, errors.ErrWorkflowInstanceNotFound) || errors2.Is(err, errors.ErrProcessInstanceNotFound) {
				log := slog.FromContext(ctx)
				log.Log(slog.InfoLevel, "processWorkflowEvents aborted due to a missing process")
				return true, nil
			} else if err != nil {
				return false, err
			}
			if err := s.XDestroyWorkflowInstance(ctx, &job, job.State, job.Error); err != nil {
				return false, fmt.Errorf("failed to destroy workflow instance whilst processing workflow events: %w", err)
			}
		}
		return true, nil
	})
	if err != nil {
		return fmt.Errorf("error starting workflow event processing: %w", err)
	}
	return nil
}

func (s *NatsService) processActivities(ctx context.Context) error {
	err := common.Process(ctx, s.js, "activity", s.closing, subj.NS(messages.WorkflowActivityAll, "*"), "ActivityConsumer", s.concurrency, func(ctx context.Context, log *slog.Logger, msg *nats.Msg) (bool, error) {
		var activity model.WorkflowState
		switch {
		case strings.HasSuffix(msg.Subject, ".State.Activity.Execute"):

		case strings.HasSuffix(msg.Subject, ".State.Activity.Complete"):
			if err := proto.Unmarshal(msg.Data, &activity); err != nil {
				return false, fmt.Errorf("failed to unmarshal state activity complete: %w", err)
			}

			if _, _, err := s.HasValidProcess(ctx, activity.ProcessInstanceId, activity.WorkflowInstanceId); errors2.Is(err, errors.ErrWorkflowInstanceNotFound) || errors2.Is(err, errors.ErrProcessInstanceNotFound) {
				log := slog.FromContext(ctx)
				log.Log(slog.InfoLevel, "processActivities aborted due to a missing process")
				return true, nil
			} else if err != nil {
				return false, err
			}
			activityID := common.TrackingID(activity.Id).ID()
			if err := s.eventActivityCompleteProcessor(ctx, &activity); err != nil {
				return false, err
			}
			err := s.deleteSavedState(activityID)
			if err != nil {
				return true, fmt.Errorf("failed to delete saved state upon activity completion: %w", err)
			}
		}

		return true, nil
	})
	if err != nil {
		return fmt.Errorf("error starting activity processing: %w", err)
	}
	return nil
}

func (s *NatsService) deleteSavedState(activityID string) error {
	if err := common.Delete(s.wfVarState, activityID); err != nil {
		return fmt.Errorf("failed to delete saved state: %w", err)
	}
	return nil
}

// CloseUserTask removes a completed user task.
func (s *NatsService) CloseUserTask(ctx context.Context, trackingID string) error {
	job := &model.WorkflowState{}
	if err := common.LoadObj(ctx, s.job, trackingID, job); err != nil {
		return fmt.Errorf("failed to load job when closing user task: %w", err)
	}

	// TODO: abstract group and user names, return all errors
	var retErr error
	allIDs := append(job.Owners, job.Groups...)
	for _, i := range allIDs {
		if err := common.UpdateObj(ctx, s.wfUserTasks, i, &model.UserTasks{}, func(msg *model.UserTasks) (*model.UserTasks, error) {
			msg.Id = remove(msg.Id, trackingID)
			return msg, nil
		}); err != nil {
			retErr = fmt.Errorf("faiiled to update user tasks object when closing user task: %w", err)
		}
	}
	return retErr
}

func (s *NatsService) openUserTask(ctx context.Context, owner string, id string) error {
	if err := common.UpdateObj(ctx, s.wfUserTasks, owner, &model.UserTasks{}, func(msg *model.UserTasks) (*model.UserTasks, error) {
		msg.Id = append(msg.Id, id)
		return msg, nil
	}); err != nil {
		return fmt.Errorf("failed to update user task object: %w", err)
	}
	return nil
}

// GetUserTaskIDs gets a list of tasks given an owner.
func (s *NatsService) GetUserTaskIDs(ctx context.Context, owner string) (*model.UserTasks, error) {
	ut := &model.UserTasks{}
	if err := common.LoadObj(ctx, s.wfUserTasks, owner, ut); err != nil {
		return nil, fmt.Errorf("failed to load user task IDs: %w", err)
	}
	return ut, nil
}

// OwnerID gets a unique identifier for a task owner.
func (s *NatsService) OwnerID(name string) (string, error) {
	if name == "" {
		name = "AnyUser"
	}
	nm, err := s.ownerID.Get(name)
	if err != nil && err != nats.ErrKeyNotFound {
		return "", fmt.Errorf("failed to get owner id: %w", err)
	}
	if nm == nil {
		id := ksuid.New().String()
		if _, err := s.ownerID.Put(name, []byte(id)); err != nil {
			return "", fmt.Errorf("failed to write owner ID: %w", err)
		}
		if _, err = s.ownerName.Put(id, []byte(name)); err != nil {
			return "", fmt.Errorf("failed to store owner name in kv: %w", err)
		}
		return id, nil
	}
	return string(nm.Value()), nil
}

// OwnerName retrieves an owner name given an ID.
func (s *NatsService) OwnerName(id string) (string, error) {
	nm, err := s.ownerName.Get(id)
	if err != nil {
		return "", fmt.Errorf("failed to get owner name for id: %w", err)
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

func (s *NatsService) expectPossibleMissingKey(ctx context.Context, msg string, err error) error {
	if errors2.Is(err, nats.ErrKeyNotFound) {
		log := slog.FromContext(ctx)
		log.Debug(msg, err)
		return nil
	}
	return fmt.Errorf("error: %w", err)
}

func (s *NatsService) listenForTimer(sCtx context.Context, js nats.JetStreamContext, closer chan struct{}, concurrency int) error {
	log := slog.FromContext(sCtx)
	subject := subj.NS("WORKFLOW.%s.Timers.>", "*")
	durable := "workflowTimers"
	for i := 0; i < concurrency; i++ {
		go func() {

			sub, err := js.PullSubscribe(subject, durable)
			if err != nil {
				log.Error("process pull subscribe error", err, slog.String("subject", subject), slog.String("durable", durable))
				return
			}
			for {
				select {
				case <-closer:
					return
				default:
				}
				reqCtx, cancel := context.WithTimeout(sCtx, 30*time.Second)
				msg, err := sub.Fetch(1, nats.Context(reqCtx))
				if err != nil {
					if errors2.Is(err, context.DeadlineExceeded) {
						cancel()
						continue
					}
					if err.Error() == "nats: Server Shutdown" || err.Error() == "nats: connection closed" {
						cancel()
						continue
					}
					// Log Error
					log.Error("message fetch error", err)
					cancel()
					continue
				}
				m := msg[0]
				//				log.Debug("Process:"+traceName, slog.String("subject", msg[0].Subject))
				cancel()
				embargoA := m.Header.Get("embargo")
				if embargoA == "" {
					embargoA = "0"
				}
				embargo, err := strconv.Atoi(embargoA)
				if err != nil {
					log.Error("bad embargo value", err)
					continue
				}
				if embargo != 0 {
					offset := time.Duration(int64(embargo) - time.Now().UnixNano())
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
					log.Error("could not unmarshal timer proto: %s", err)
					err := msg[0].Ack()
					if err != nil {
						log.Error("could not dispose of timer message after unmarshal error: %s", err)
					}
					continue
				}
				wi, err := s.hasValidInstance(sCtx, state.WorkflowInstanceId)
				if errors2.Is(err, errors.ErrWorkflowInstanceNotFound) {
					log := slog.FromContext(sCtx)
					log.Log(slog.InfoLevel, "listenForTimer aborted due to a missing instance")
					continue
				} else if err != nil {
					continue
				}
				var cid string
				if cid = msg[0].Header.Get(logx.CorrelationHeader); cid == "" {
					log.Error("correlation key missing", errors.ErrMissingCorrelation)
					return
				}

				ctx, log := logx.NatsMessageLoggingEntrypoint(sCtx, "shar-server", msg[0].Header)
				ctx, err = header.FromMsgHeaderToCtx(ctx, m.Header)
				if err != nil {
					log.Error("failed to get header values from incoming process message", &errors.ErrWorkflowFatal{Err: err})
					if err := msg[0].Ack(); err != nil {
						log.Error("processing failed to ack", err)
					}
					continue
				}
				if strings.HasSuffix(msg[0].Subject, ".Timers.ElementExecute") {
					pi, err := s.GetProcessInstance(ctx, state.ProcessInstanceId)
					if errors2.Is(err, errors.ErrProcessInstanceNotFound) {
						if err := msg[0].Ack(); err != nil {
							log.Error("failed to ack message after process instance not found", err)
							continue
						}
						continue
					}
					wf, err := s.GetWorkflow(ctx, pi.WorkflowId)
					if err != nil {
						log.Error("failed to get workflow", err)
						continue
					}
					activityID := common.TrackingID(state.Id).ID()
					_, err = s.GetOldState(ctx, activityID)
					if errors2.Is(err, errors.ErrStateNotFound) {
						if err := msg[0].Ack(); err != nil {
							log.Error("failed to ack message after state not found", err)
							continue
						}
					}
					if err != nil {
						return
					}
					els := common.ElementTable(wf)
					parent := common.TrackingID(state.Id).Pop()
					if err := s.traversalFunc(ctx, pi, parent, &model.Targets{Target: []*model.Target{{Id: "timer-target", Target: *state.Execute}}}, els, state); err != nil {
						log.Error("failed to traverse", err)
						continue
					}
					if err := s.PublishWorkflowState(ctx, subj.NS(messages.WorkflowActivityAbort, "default"), state); err != nil {
						if err != nil {
							continue
						}
					}

					if err = msg[0].Ack(); err != nil {
						log.Warn("failed to ack after timer redirect", err)
					}
					continue
				}
				ack, delay, err := s.messageProcessor(ctx, state, wi, int64(embargo))
				if err != nil {
					if errors.IsWorkflowFatal(err) {
						if err := msg[0].Ack(); err != nil {
							log.Error("failed to ack after a fatal error in message processing: %s", err)
						}
						log.Error("a fatal error occurred processing a message: %s", err)
						continue
					}
					log.Error("an error occurred processing a message: %s", err)
					continue
				}
				if ack {
					err := msg[0].Ack()
					if err != nil {
						log.Error("could not ack after message processing: %s", err)
						continue
					}
				} else {
					if delay > 0 {
						err := msg[0].NakWithDelay(time.Duration(delay))
						if err != nil {
							log.Error("could not nak message with delay: %s", err)
							continue
						}
					} else {
						err := msg[0].Nak()
						if err != nil {
							log.Error("could not nak message: %s", err)
							continue
						}
					}
				}
			}
		}()
	}
	return nil
}

// GetOldState gets a task state given its tracking ID.
func (s *NatsService) GetOldState(ctx context.Context, id string) (*model.WorkflowState, error) {
	oldState := &model.WorkflowState{}
	err := common.LoadObj(ctx, s.wfVarState, id, oldState)
	if err == nil {
		return oldState, nil
	} else if errors2.Is(err, nats.ErrKeyNotFound) {
		return nil, fmt.Errorf("get old state failed to load object: %w", errors.ErrStateNotFound)
	}
	return nil, fmt.Errorf("error retrieving task state: %w", err)
}

func (s *NatsService) processLaunch(ctx context.Context) error {
	err := common.Process(ctx, s.js, "launch", s.closing, subj.NS(messages.WorkflowJobLaunchExecute, "*"), "LaunchConsumer", s.concurrency, func(ctx context.Context, log *slog.Logger, msg *nats.Msg) (bool, error) {
		var job model.WorkflowState
		if err := proto.Unmarshal(msg.Data, &job); err != nil {
			return false, fmt.Errorf("failed to unmarshal during process launch: %w", err)
		}
		if _, _, err := s.HasValidProcess(ctx, job.ProcessInstanceId, job.WorkflowInstanceId); errors2.Is(err, errors.ErrWorkflowInstanceNotFound) || errors2.Is(err, errors.ErrProcessInstanceNotFound) {
			log := slog.FromContext(ctx)
			log.Log(slog.InfoLevel, "processLaunch aborted due to a missing process")
			return true, err
		} else if err != nil {
			return false, err
		}
		if err := s.launchFunc(ctx, &job); err != nil {
			return false, fmt.Errorf("failed to execute launch function: %w", err)
		}
		return true, nil
	})
	if err != nil {
		return fmt.Errorf("failed to start process launch processor: %w", err)
	}
	return nil
}

func (s *NatsService) processJobAbort(ctx context.Context) error {
	err := common.Process(ctx, s.js, "abort", s.closing, subj.NS(messages.WorkFlowJobAbortAll, "*"), "JobAbortConsumer", s.concurrency, func(ctx context.Context, log *slog.Logger, msg *nats.Msg) (bool, error) {
		var state model.WorkflowState
		if err := proto.Unmarshal(msg.Data, &state); err != nil {
			return false, fmt.Errorf("job abort consumer failed to unmarshal state: %w", err)
		}
		if _, _, err := s.HasValidProcess(ctx, state.ProcessInstanceId, state.WorkflowInstanceId); errors2.Is(err, errors.ErrWorkflowInstanceNotFound) || errors2.Is(err, errors.ErrProcessInstanceNotFound) {
			log := slog.FromContext(ctx)
			log.Log(slog.InfoLevel, "processJobAbort aborted due to a missing process")
			return true, err
		} else if err != nil {
			return false, err
		}
		//TODO: Make these idempotently work given missing values
		switch {
		case strings.Contains(msg.Subject, ".State.Job.Abort.ServiceTask"):
			if err := s.deleteJob(ctx, &state); err != nil {
				return false, fmt.Errorf("failed to delete job during service task abort: %w", err)
			}
		default:
			return true, nil
		}
		return true, nil
	})
	if err != nil {
		return fmt.Errorf("failed to start job abort processor: %w", err)
	}
	return nil
}

func (s *NatsService) processProcessComplete(ctx context.Context) error {
	err := common.Process(ctx, s.js, "processComplete", s.closing, subj.NS(messages.WorkflowProcessComplete, "*"), "ProcessCompleteConsumer", s.concurrency, func(ctx context.Context, log *slog.Logger, msg *nats.Msg) (bool, error) {
		var state model.WorkflowState
		if err := proto.Unmarshal(msg.Data, &state); err != nil {
			return false, fmt.Errorf("failed to unmarshal during general abort processor: %w", err)
		}
		pi, wi, err := s.HasValidProcess(ctx, state.ProcessInstanceId, state.WorkflowInstanceId)
		if errors2.Is(err, errors.ErrWorkflowInstanceNotFound) || errors2.Is(err, errors.ErrProcessInstanceNotFound) {
			log := slog.FromContext(ctx)
			log.Log(slog.InfoLevel, "processProcessComplete aborted due to a missing process")
			return true, err
		} else if err != nil {
			return false, err
		}
		state.State = model.CancellationState_completed
		if err := s.DestroyProcessInstance(ctx, &state, pi, wi); err != nil {
			return false, fmt.Errorf("failed to delete prcess: %w", err)
		}
		return true, nil
	})
	if err != nil {
		return fmt.Errorf("failed to start general abort processor: %w", err)
	}
	return nil

}

func (s *NatsService) processGeneralAbort(ctx context.Context) error {
	err := common.Process(ctx, s.js, "abort", s.closing, subj.NS(messages.WorkflowGeneralAbortAll, "*"), "GeneralAbortConsumer", s.concurrency, func(ctx context.Context, log *slog.Logger, msg *nats.Msg) (bool, error) {
		var state model.WorkflowState
		if err := proto.Unmarshal(msg.Data, &state); err != nil {
			return false, fmt.Errorf("failed to unmarshal during general abort processor: %w", err)
		}
		//TODO: Make these idempotently work given missing values
		switch {
		case strings.HasSuffix(msg.Subject, ".State.Activity.Abort"):
			if err := s.deleteActivity(&state); err != nil {
				return false, fmt.Errorf("failed to delete activity during general abort processor: %w", err)
			}
		case strings.HasSuffix(msg.Subject, ".State.Workflow.Abort"):
			if err := s.XDestroyWorkflowInstance(ctx, &state, model.CancellationState_terminated, state.Error); err != nil {
				return false, fmt.Errorf("failed to delete workflow during general abort processor: %w", err)
			}
		default:
			return true, nil
		}
		return true, nil
	})
	if err != nil {
		return fmt.Errorf("failed to start general abort processor: %w", err)
	}
	return nil
}

func (s *NatsService) deleteActivity(state *model.WorkflowState) error {
	if err := s.deleteSavedState(common.TrackingID(state.Id).ID()); err != nil && !errors2.Is(err, nats.ErrKeyNotFound) {
		return fmt.Errorf("could not delete activity: %w", err)
	}
	return nil
}

func (s *NatsService) deleteJob(ctx context.Context, state *model.WorkflowState) error {
	if err := s.DeleteJob(ctx, common.TrackingID(state.Id).ID()); err != nil && !errors2.Is(err, nats.ErrKeyNotFound) {
		return fmt.Errorf("could not delete job: %w", err)
	}
	if activityState, err := s.GetOldState(ctx, common.TrackingID(state.Id).Pop().ID()); err != nil && !errors2.Is(err, errors.ErrStateNotFound) {
		return fmt.Errorf("failed to fetch old state during delete job: %w", err)
	} else if err == nil {
		if err := s.PublishWorkflowState(ctx, subj.NS(messages.WorkflowActivityAbort, "default"), activityState); err != nil {
			return fmt.Errorf("failed to publish activity abort during delete job: %w", err)
		}
	}
	return nil
}

// SaveState saves the task state.
func (s *NatsService) SaveState(ctx context.Context, id string, state *model.WorkflowState) error {
	saveState := proto.Clone(state).(*model.WorkflowState)
	saveState.Id = common.TrackingID(saveState.Id).Pop().Push(id)
	data, err := proto.Marshal(saveState)
	if err != nil {
		return fmt.Errorf("failed to unmarshal saved state: %w", err)
	}
	if err := common.Save(ctx, s.wfVarState, id, data); err != nil {
		return fmt.Errorf("failed to save state: %w", err)
	}
	return nil
}

// CreateProcessInstance creates a new instance of a process and attaches it to the workflow instance.
func (s *NatsService) CreateProcessInstance(ctx context.Context, workflowInstanceID string, parentProcessID string, parentElementID string, processName string) (*model.ProcessInstance, error) {
	id := ksuid.New().String()
	pi := &model.ProcessInstance{
		ProcessInstanceId:  id,
		ProcessName:        processName,
		WorkflowInstanceId: workflowInstanceID,
		ParentProcessId:    &parentProcessID,
		ParentElementId:    &parentElementID,
	}
	wfi, err := s.GetWorkflowInstance(ctx, workflowInstanceID)
	if err != nil {
		return nil, fmt.Errorf("create process instance failed to get workflow instance: %w", err)
	}
	pi.WorkflowName = wfi.WorkflowName
	pi.WorkflowId = wfi.WorkflowId
	err = common.SaveObj(ctx, s.wfProcessInstance, pi.ProcessInstanceId, pi)
	if err != nil {
		return nil, fmt.Errorf("create process instance failed to save process instance: %w", err)
	}
	err = common.UpdateObj(ctx, s.wfInstance, workflowInstanceID, wfi, func(v *model.WorkflowInstance) (*model.WorkflowInstance, error) {
		v.ProcessInstanceId = append(v.ProcessInstanceId, pi.ProcessInstanceId)
		return v, nil
	})
	if err != nil {
		return nil, fmt.Errorf("create process instance failed to update workflow instance: %w", err)
	}
	return pi, nil
}

// GetProcessInstance returns a process instance for a given process ID
func (s *NatsService) GetProcessInstance(ctx context.Context, processInstanceID string) (*model.ProcessInstance, error) {
	pi := &model.ProcessInstance{}
	err := common.LoadObj(ctx, s.wfProcessInstance, processInstanceID, pi)
	if errors2.Is(err, nats.ErrKeyNotFound) {
		return nil, fmt.Errorf("get process instance failed to load instance: %w", errors.ErrProcessInstanceNotFound)
	}
	if err != nil {
		return nil, fmt.Errorf("get process instance failed to load instance: %w", err)
	}
	return pi, nil
}

// DestroyProcessInstance deletes a process instance and removes the workflow instance dependent on all process instances being satisfied.
func (s *NatsService) DestroyProcessInstance(ctx context.Context, state *model.WorkflowState, pi *model.ProcessInstance, wi *model.WorkflowInstance) error {
	wfi := &model.WorkflowInstance{}
	err := common.UpdateObj(ctx, s.wfInstance, wi.WorkflowInstanceId, wfi, func(v *model.WorkflowInstance) (*model.WorkflowInstance, error) {
		v.ProcessInstanceId = remove(v.ProcessInstanceId, pi.ProcessInstanceId)
		return v, nil
	})
	if err != nil {
		return fmt.Errorf("destroy process instance failed to update workflow instance: %w", err)
	}
	err = common.Delete(s.wfProcessInstance, pi.ProcessInstanceId)
	// TODO: Key not found
	if err != nil {
		return fmt.Errorf("destroy process instance failed to delete process instance: %w", err)
	}
	def, err := s.GetWorkflow(ctx, pi.WorkflowId)
	if err != nil {
		return fmt.Errorf("destroy process instance failed to fetch workflow: %w", err)
	}
	var lock bool
	for _, p := range def.Process {
		_, satisfied := wi.SatisfiedProcesses[p.Name]
		if p.Metadata.TimedStart && !satisfied {
			lock = true
			break
		}
	}
	if len(wfi.ProcessInstanceId) == 0 && !lock {
		if err := s.PublishWorkflowState(ctx, messages.WorkflowInstanceComplete, state); err != nil {
			return fmt.Errorf("destroy process instance failed initiaite completing workflow instance: %w", err)
		}
	}
	return nil
}

func (s *NatsService) populateMetadata(wf *model.Workflow) {
	for _, process := range wf.Process {
		process.Metadata = &model.Metadata{}
		for _, elem := range process.Elements {
			if elem.Type == element.TimedStartEvent {
				process.Metadata.TimedStart = true
			}
		}
	}
}

// SatisfyProcess sets a process as "satisfied" i.e. it may no longer trigger.
func (s *NatsService) SatisfyProcess(ctx context.Context, workflowInstance *model.WorkflowInstance, processName string) error {
	err := common.UpdateObj(ctx, s.wfInstance, workflowInstance.WorkflowInstanceId, workflowInstance, func(wi *model.WorkflowInstance) (*model.WorkflowInstance, error) {
		wi.SatisfiedProcesses[processName] = true
		return wi, nil
	})
	if err != nil {
		return fmt.Errorf("failed to satify process: %w", err)
	}
	return nil
}
