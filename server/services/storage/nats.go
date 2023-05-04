package storage

import (
	"bytes"
	"context"
	errors2 "errors"
	"fmt"
	"github.com/nats-io/nats.go"
	"github.com/segmentio/ksuid"
	"gitlab.com/shar-workflow/shar/common"
	"gitlab.com/shar-workflow/shar/common/element"
	"gitlab.com/shar-workflow/shar/common/header"
	"gitlab.com/shar-workflow/shar/common/logx"
	"gitlab.com/shar-workflow/shar/common/setup"
	"gitlab.com/shar-workflow/shar/common/subj"
	"gitlab.com/shar-workflow/shar/common/workflow"
	"gitlab.com/shar-workflow/shar/model"
	"gitlab.com/shar-workflow/shar/server/errors"
	"gitlab.com/shar-workflow/shar/server/errors/keys"
	"gitlab.com/shar-workflow/shar/server/messages"
	"gitlab.com/shar-workflow/shar/server/services"
	"golang.org/x/exp/slog"
	"google.golang.org/protobuf/proto"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Nats contains the engine functions that communicate with NATS.
type Nats struct {
	js                             nats.JetStreamContext
	txJS                           nats.JetStreamContext
	eventProcessor                 services.EventProcessorFunc
	eventJobCompleteProcessor      services.CompleteJobProcessorFunc
	traversalFunc                  services.TraversalFunc
	launchFunc                     services.LaunchFunc
	messageProcessor               services.MessageProcessorFunc
	storageType                    nats.StorageType
	concurrency                    int
	closing                        chan struct{}
	wfInstance                     nats.KeyValue
	wfProcessInstance              nats.KeyValue
	wfMessageInterest              nats.KeyValue
	wfUserTasks                    nats.KeyValue
	wfVarState                     nats.KeyValue
	wf                             nats.KeyValue
	wfVersion                      nats.KeyValue
	wfTracking                     nats.KeyValue
	job                            nats.KeyValue
	ownerName                      nats.KeyValue
	ownerID                        nats.KeyValue
	wfClientTask                   nats.KeyValue
	wfGateway                      nats.KeyValue
	conn                           common.NatsConn
	txConn                         common.NatsConn
	workflowStats                  *model.WorkflowStats
	statsMx                        sync.Mutex
	wfName                         nats.KeyValue
	wfHistory                      nats.KeyValue
	publishTimeout                 time.Duration
	eventActivityCompleteProcessor services.CompleteActivityProcessorFunc
	allowOrphanServiceTasks        bool
	completeActivityFunc           services.CompleteActivityFunc
	abortFunc                      services.AbortFunc
	wfLock                         nats.KeyValue
	wfMsgTypes                     nats.KeyValue
}

// WorkflowStats obtains the running counts for the engine
func (s *Nats) WorkflowStats() *model.WorkflowStats {
	s.statsMx.Lock()
	defer s.statsMx.Unlock()
	return s.workflowStats
}

// ListWorkflows returns a list of all the workflows in SHAR.
func (s *Nats) ListWorkflows(ctx context.Context) (chan *model.ListWorkflowResult, chan error) {
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

// New creates a new instance of the NATS communication layer.
func New(conn common.NatsConn, txConn common.NatsConn, storageType nats.StorageType, concurrency int, allowOrphanServiceTasks bool) (*Nats, error) {
	if concurrency < 1 || concurrency > 200 {
		return nil, fmt.Errorf("invalid concurrency: %w", errors2.New("invalid concurrency set"))
	}

	js, err := conn.JetStream()
	if err != nil {
		return nil, fmt.Errorf("open jetstream: %w", err)
	}
	txJS, err := txConn.JetStream()
	if err != nil {
		return nil, fmt.Errorf("open jetstream: %w", err)
	}
	ms := &Nats{
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
	ctx := context.Background()
	if err := setup.EnsureWorkflowStream(ctx, conn, js, storageType); err != nil {
		return nil, fmt.Errorf("set up nats queue insfrastructure: %w", err)
	}

	kvs := make(map[string]*nats.KeyValue)

	kvs[messages.KvWfName] = &ms.wfName
	kvs[messages.KvInstance] = &ms.wfInstance
	kvs[messages.KvTracking] = &ms.wfTracking
	kvs[messages.KvDefinition] = &ms.wf
	kvs[messages.KvJob] = &ms.job
	kvs[messages.KvVersion] = &ms.wfVersion
	kvs[messages.KvMessageInterest] = &ms.wfMessageInterest
	kvs[messages.KvUserTask] = &ms.wfUserTasks
	kvs[messages.KvOwnerID] = &ms.ownerID
	kvs[messages.KvOwnerName] = &ms.ownerName
	kvs[messages.KvClientTaskID] = &ms.wfClientTask
	kvs[messages.KvVarState] = &ms.wfVarState
	kvs[messages.KvProcessInstance] = &ms.wfProcessInstance
	kvs[messages.KvGateway] = &ms.wfGateway
	kvs[messages.KvHistory] = &ms.wfHistory
	kvs[messages.KvLock] = &ms.wfLock
	kvs[messages.KvMessageTypes] = &ms.wfMsgTypes
	ks := make([]string, 0, len(kvs))
	for k := range kvs {
		ks = append(ks, k)
	}
	if err := common.EnsureBuckets(js, storageType, ks); err != nil {
		return nil, fmt.Errorf("ensure the KV buckets: %w", err)
	}

	for k, v := range kvs {
		kv, err := js.KeyValue(k)
		if err != nil {
			return nil, fmt.Errorf("open %s KV: %w", k, err)
		}
		*v = kv
	}

	kickConsumer, err := js.ConsumerInfo("WORKFLOW", "MessageKickConsumer")
	if err != nil {
		return nil, fmt.Errorf("obtaining message kick consumer information: %w", err)
	}
	if int(kickConsumer.NumPending)+kickConsumer.NumAckPending+kickConsumer.NumWaiting == 0 {
		if lock, err := common.Lock(ms.wfLock, "MessageKick"); err != nil {
			return nil, fmt.Errorf("obtaining lock for message kick consumer: %w", err)
		} else if lock {
			defer func() {
				// clear the lock out of courtesy
				if err := common.UnLock(ms.wfLock, "MessageKick"); err != nil {
					slog.Warn("releasing lock for message kick consumer")
				}
			}()
			if _, err := ms.js.Publish(messages.WorkflowMessageKick, []byte{}); err != nil {
				return nil, fmt.Errorf("starting message kick timer: %w", err)
			}
		}
	}

	return ms, nil
}

// StartProcessing begins listening to all the message processing queues.
func (s *Nats) StartProcessing(ctx context.Context) error {

	if err := s.processTraversals(ctx); err != nil {
		return fmt.Errorf("start traversals handler: %w", err)
	}
	if err := s.processJobAbort(ctx); err != nil {
		return fmt.Errorf("start job abort handler: %w", err)
	}
	if err := s.processGeneralAbort(ctx); err != nil {
		return fmt.Errorf("general abort handler: %w", err)
	}
	if err := s.processTracking(ctx); err != nil {
		return fmt.Errorf("start tracking handler: %w", err)
	}
	if err := s.processWorkflowEvents(ctx); err != nil {
		return fmt.Errorf("start workflow events handler: %w", err)
	}
	if err := s.processMessages(ctx); err != nil {
		return fmt.Errorf("start process messages handler: %w", err)
	}
	if err := s.listenForTimer(ctx, s.js, s.closing, 4); err != nil {
		return fmt.Errorf("start timer handler: %w", err)
	}
	if err := s.processCompletedJobs(ctx); err != nil {
		return fmt.Errorf("start completed jobs handler: %w", err)
	}
	if err := s.processActivities(ctx); err != nil {
		return fmt.Errorf("start activities handler: %w", err)
	}
	if err := s.processLaunch(ctx); err != nil {
		return fmt.Errorf("start launch handler: %w", err)
	}
	if err := s.processProcessComplete(ctx); err != nil {
		return fmt.Errorf("start process complete handler: %w", err)
	}
	if err := s.processGatewayActivation(ctx); err != nil {
		return fmt.Errorf("start gateway execute handler: %w", err)
	}
	if err := s.processAwaitMessageExecute(ctx); err != nil {
		return fmt.Errorf("start await message handler: %w", err)
	}
	if err := s.processGatewayExecute(ctx); err != nil {
		return fmt.Errorf("start gateway execute handler: %w", err)
	}
	if err := s.messageKick(ctx); err != nil {
		return fmt.Errorf("starting message kick: %w", err)
	}
	return nil
}

// StoreWorkflow stores a workflow definition and returns a unique ID
func (s *Nats) StoreWorkflow(ctx context.Context, wf *model.Workflow) (string, error) {

	// Populate Metadata
	s.populateMetadata(wf)

	// get this workflow name if it has already been registered
	_, err := s.wfName.Get(wf.Name)
	if errors2.Is(err, nats.ErrKeyNotFound) {
		wfNameID := ksuid.New().String()
		_, err = s.wfName.Put(wf.Name, []byte(wfNameID))
		if err != nil {
			return "", fmt.Errorf("store the workflow id during store workflow: %w", err)
		}
	} else if err != nil {
		return "", fmt.Errorf("get an existing workflow id: %w", err)
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
			return nil, fmt.Errorf("save workflow: %s", wf.Name)
		}
		v.Version = append(v.Version, &model.WorkflowVersion{Id: wfID, Sha256: hash, Number: int32(n) + 1})
		return v, nil
	}); err != nil {
		return "", fmt.Errorf("update workflow version for: %s", wf.Name)
	}

	if !newWf {
		return wfID, nil
	}

	if err := s.ensureMessageBuckets(ctx, wf); err != nil {
		return "", fmt.Errorf("create workflow message buckets: %w", err)
	}

	for _, i := range wf.Process {
		for _, j := range i.Elements {
			if j.Type == element.ServiceTask {
				id := ksuid.New().String()
				_, err := s.wfClientTask.Get(j.Execute)
				if err != nil && errors2.Is(err, nats.ErrKeyNotFound) {
					_, err := s.wfClientTask.Put(j.Execute, []byte(id))
					if err != nil {
						return "", fmt.Errorf("add task to registry: %w", err)
					}

					jxCfg := &nats.ConsumerConfig{
						Durable:       "ServiceTask_" + id,
						Description:   "",
						FilterSubject: subj.NS(messages.WorkflowJobServiceTaskExecute, "default") + "." + id,
						AckPolicy:     nats.AckExplicitPolicy,
					}

					if err = ensureConsumer(s.js, "WORKFLOW", jxCfg); err != nil {
						return "", fmt.Errorf("add service task consumer: %w", err)
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
		return fmt.Errorf("call to get consumer info in ensure consumer: %w", err)
	}
	return nil
}

// GetWorkflow - retrieves a workflow model given its ID
func (s *Nats) GetWorkflow(ctx context.Context, workflowID string) (*model.Workflow, error) {
	wf := &model.Workflow{}
	if err := common.LoadObj(ctx, s.wf, workflowID, wf); errors2.Is(err, nats.ErrKeyNotFound) {
		return nil, fmt.Errorf("get workflow failed to load object: %w", errors.ErrWorkflowNotFound)

	} else if err != nil {
		return nil, fmt.Errorf("load workflow from KV: %w", err)
	}
	return wf, nil
}

// GetWorkflowVersions - returns a list of versions for a given workflow.
func (s *Nats) GetWorkflowVersions(ctx context.Context, workflowName string) (*model.WorkflowVersions, error) {
	ver := &model.WorkflowVersions{}
	if err := common.LoadObj(ctx, s.wfVersion, workflowName, ver); errors2.Is(err, nats.ErrKeyNotFound) {
		return nil, fmt.Errorf("get workflow versions failed to load object: %w", errors.ErrWorkflowVersionNotFound)

	} else if err != nil {
		return nil, fmt.Errorf("load workflow from KV: %w", err)
	}
	return ver, nil
}

// CreateWorkflowInstance given a workflow, starts a new workflow instance and returns its ID
func (s *Nats) CreateWorkflowInstance(ctx context.Context, wfInstance *model.WorkflowInstance) (*model.WorkflowInstance, error) {
	wfiID := ksuid.New().String()
	log := logx.FromContext(ctx)
	log.Info("creating workflow instance", slog.String(keys.WorkflowInstanceID, wfiID))
	wfInstance.WorkflowInstanceId = wfiID
	wfInstance.ProcessInstanceId = []string{}
	wfInstance.SatisfiedProcesses = map[string]bool{".": true}
	if err := common.SaveObj(ctx, s.wfInstance, wfiID, wfInstance); err != nil {
		return nil, fmt.Errorf("save workflow instance object to KV: %w", err)
	}
	wf, err := s.GetWorkflow(ctx, wfInstance.WorkflowId)
	if err != nil {
		return nil, fmt.Errorf("save workflow instance object to KV: %w", err)
	}
	for _, m := range wf.Messages {
		if err := common.EnsureBucket(s.js, s.storageType, subj.NS("MsgTx_%s_", "default")+m.Name, 0); err != nil {
			return nil, fmt.Errorf("ensuring bucket '%s':%w", m.Name, err)
		}
	}
	s.incrementWorkflowStarted()
	return wfInstance, nil
}

// GetWorkflowInstance retrieves workflow instance given its ID.
func (s *Nats) GetWorkflowInstance(ctx context.Context, workflowInstanceID string) (*model.WorkflowInstance, error) {
	wfi := &model.WorkflowInstance{}
	if err := common.LoadObj(ctx, s.wfInstance, workflowInstanceID, wfi); errors2.Is(err, nats.ErrKeyNotFound) {
		return nil, fmt.Errorf("get workflow instance failed to load object: %w", errors.ErrWorkflowInstanceNotFound)
	} else if err != nil {
		return nil, fmt.Errorf("load workflow instance from KV: %w", err)
	}
	return wfi, nil
}

// GetServiceTaskRoutingKey gets a unique ID for a service task that can be used to listen for its activation.
func (s *Nats) GetServiceTaskRoutingKey(ctx context.Context, taskName string, requestedId string) (string, error) {
	var b []byte
	var err error
	if b, err = common.Load(ctx, s.wfClientTask, taskName); err != nil && errors2.Is(err, nats.ErrKeyNotFound) {
		if !s.allowOrphanServiceTasks {
			return "", fmt.Errorf("get service task key. key not present: %w", err)
		}
		var id string
		if requestedId == "" {
			id = ksuid.New().String()
		} else {
			id = requestedId
		}
		_, err := s.wfClientTask.Put(taskName, []byte(id))
		if err != nil {
			return "", fmt.Errorf("register service task key: %w", err)
		}
		return id, nil
	} else if err != nil {
		return "", fmt.Errorf("get service task key: %w", err)
	}
	return string(b), nil
}

// XDestroyWorkflowInstance terminates a running workflow instance with a cancellation reason and error
func (s *Nats) XDestroyWorkflowInstance(ctx context.Context, state *model.WorkflowState) error {
	log := logx.FromContext(ctx)
	log.Info("destroying workflow instance", slog.String(keys.WorkflowInstanceID, state.WorkflowInstanceId))
	// Get the workflow instance
	wfi := &model.WorkflowInstance{}
	if err := common.LoadObj(ctx, s.wfInstance, state.WorkflowInstanceId, wfi); err != nil {
		log.Warn("fetch workflow instance",
			slog.String(keys.WorkflowInstanceID, state.WorkflowInstanceId),
		)
		return s.expectPossibleMissingKey(ctx, "fetching workflow instance", err)
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
			log.Warn("fetch workflow definition",
				slog.String(keys.WorkflowInstanceID, wfi.WorkflowInstanceId),
				slog.String(keys.WorkflowID, wfi.WorkflowId),
				slog.String(keys.WorkflowName, wf.Name),
			)
		}
	}
	tState := common.CopyWorkflowState(state)

	if tState.Error != nil {
		tState.State = model.CancellationState_errored
	}

	if err := s.deleteWorkflowInstance(ctx, tState); err != nil {
		return fmt.Errorf("delete workflow state whilst destroying workflow instance: %w", err)
	}
	s.incrementWorkflowCompleted()
	return nil
}

func (s *Nats) deleteWorkflowInstance(ctx context.Context, state *model.WorkflowState) error {
	if err := s.wfInstance.Delete(state.WorkflowInstanceId); err != nil && !errors2.Is(err, nats.ErrKeyNotFound) {
		return fmt.Errorf("delete workflow instance: %w", err)
	}

	//TODO: Loop through all messages checking for process subscription and remove

	if err := s.wfTracking.Delete(state.WorkflowInstanceId); err != nil && !errors2.Is(err, nats.ErrKeyNotFound) {
		return fmt.Errorf("delete workflow tracking: %w", err)
	}
	if err := s.PublishWorkflowState(ctx, messages.WorkflowInstanceTerminated, state); err != nil {
		return fmt.Errorf("send workflow terminate message: %w", err)
	}
	return nil
}

// GetLatestVersion queries the workflow versions table for the latest entry
func (s *Nats) GetLatestVersion(ctx context.Context, workflowName string) (string, error) {
	v := &model.WorkflowVersions{}
	if err := common.LoadObj(ctx, s.wfVersion, workflowName, v); errors2.Is(err, nats.ErrKeyNotFound) {
		return "", fmt.Errorf("get latest workflow version: %w", errors.ErrWorkflowNotFound)
	} else if err != nil {
		return "", fmt.Errorf("load object whist getting latest versiony: %w", err)
	} else {
		return v.Version[len(v.Version)-1].Id, nil
	}
}

// CreateJob stores a workflow task state.
func (s *Nats) CreateJob(ctx context.Context, job *model.WorkflowState) (string, error) {
	tid := ksuid.New().String()
	job.Id = common.TrackingID(job.Id).Push(tid)
	if err := common.SaveObj(ctx, s.job, tid, job); err != nil {
		return "", fmt.Errorf("save job to KV: %w", err)
	}
	return tid, nil
}

// GetJob gets a workflow task state.
func (s *Nats) GetJob(ctx context.Context, trackingID string) (*model.WorkflowState, error) {
	job := &model.WorkflowState{}
	if err := common.LoadObj(ctx, s.job, trackingID, job); err == nil {
		return job, nil
	} else if errors2.Is(err, nats.ErrKeyNotFound) {
		return nil, fmt.Errorf("get job failed to load workflow object: %w", errors.ErrJobNotFound)
	} else if err != nil {
		return nil, fmt.Errorf("load job from KV: %w", err)
	} else {
		return job, nil
	}
}

// DeleteJob removes a workflow task state.
func (s *Nats) DeleteJob(_ context.Context, trackingID string) error {
	if err := common.Delete(s.job, trackingID); err != nil {
		return fmt.Errorf("delete job: %w", err)
	}
	return nil
}

// ListWorkflowInstance returns a list of running workflows and versions given a workflow ID
func (s *Nats) ListWorkflowInstance(ctx context.Context, workflowName string) (chan *model.ListWorkflowInstanceResult, chan error) {
	log := logx.FromContext(ctx)
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
		log := logx.FromContext(ctx)
		log.Error("obtaining keys", err)
		return nil, errs
	}
	go func(keys []string) {
		for _, k := range keys {
			v := &model.WorkflowInstance{}
			err := common.LoadObj(ctx, s.wfInstance, k, v)
			if wv, ok := ver[v.WorkflowId]; ok {
				if err != nil && err != nats.ErrKeyNotFound {
					errs <- err
					log.Error("loading object", err)
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
func (s *Nats) ListWorkflowInstanceProcesses(ctx context.Context, id string) ([]string, error) {
	v := &model.WorkflowInstance{}
	err := common.LoadObj(ctx, s.wfInstance, id, v)
	if err != nil {
		return nil, fmt.Errorf("load workflow instance from KV: %w", err)
	}
	return v.ProcessInstanceId, nil
}

// GetProcessInstanceStatus returns a list of workflow statuses for the specified process instance ID.
func (s *Nats) GetProcessInstanceStatus(ctx context.Context, id string) ([]*model.WorkflowState, error) {
	v := &model.WorkflowState{}
	err := common.LoadObj(ctx, s.wfProcessInstance, id, v)
	if err != nil {
		return nil, fmt.Errorf("function GetProcessInstanceStatus failed to load from KV: %w", err)
	}
	return []*model.WorkflowState{v}, nil
}

// SetEventProcessor sets the callback for processing workflow activities.
func (s *Nats) SetEventProcessor(processor services.EventProcessorFunc) {
	s.eventProcessor = processor
}

// SetMessageProcessor sets the callback used to create new workflow instances based on a timer.
func (s *Nats) SetMessageProcessor(processor services.MessageProcessorFunc) {
	s.messageProcessor = processor
}

// SetCompleteJobProcessor sets the callback for completed tasks.
func (s *Nats) SetCompleteJobProcessor(processor services.CompleteJobProcessorFunc) {
	s.eventJobCompleteProcessor = processor
}

// SetCompleteActivityProcessor sets the callback fired when an activity completes.
func (s *Nats) SetCompleteActivityProcessor(processor services.CompleteActivityProcessorFunc) {
	s.eventActivityCompleteProcessor = processor
}

// SetLaunchFunc sets the callback used to start child workflows.
func (s *Nats) SetLaunchFunc(processor services.LaunchFunc) {
	s.launchFunc = processor
}

// SetTraversalProvider sets the callback used to handle traversals.
func (s *Nats) SetTraversalProvider(provider services.TraversalFunc) {
	s.traversalFunc = provider
}

// SetCompleteActivity sets the callback which generates complete activity events.
func (s *Nats) SetCompleteActivity(processor services.CompleteActivityFunc) {
	s.completeActivityFunc = processor
}

// SetAbort sets the function called when a workflow object aborts.
func (s *Nats) SetAbort(processor services.AbortFunc) {
	s.abortFunc = processor
}

// PublishWorkflowState publishes a SHAR state object to a given subject
func (s *Nats) PublishWorkflowState(ctx context.Context, stateName string, state *model.WorkflowState, opts ...PublishOpt) error {
	c := &publishOptions{}
	for _, i := range opts {
		i.Apply(c)
	}
	state.UnixTimeNano = time.Now().UnixNano()
	msg := nats.NewMsg(subj.NS(stateName, "default"))
	msg.Header.Set("embargo", strconv.Itoa(c.Embargo))
	b, err := proto.Marshal(state)
	if err != nil {
		return fmt.Errorf("marshal proto during publish workflow state: %w", err)
	}
	msg.Data = b
	if err := header.FromCtxToMsgHeader(ctx, &msg.Header); err != nil {
		return fmt.Errorf("add header to published workflow state: %w", err)
	}
	pubCtx, cancel := context.WithTimeout(ctx, s.publishTimeout)
	defer cancel()
	if c.ID == "" {
		c.ID = ksuid.New().String()
	}

	if _, err := s.txJS.PublishMsg(msg, nats.Context(pubCtx), nats.MsgId(c.ID)); err != nil {
		log := logx.FromContext(ctx)
		log.Error("publish message", err, slog.String("nats.msg.id", c.ID), slog.Any("state", state), slog.String("subject", msg.Subject))
		return fmt.Errorf("publish workflow state message: %w", err)
	}
	if stateName == subj.NS(messages.WorkflowJobUserTaskExecute, "default") {
		for _, i := range append(state.Owners, state.Groups...) {
			if err := s.openUserTask(ctx, i, common.TrackingID(state.Id).ID()); err != nil {
				return fmt.Errorf("open user task during publish workflow state: %w", err)
			}
		}
	}
	return nil
}

// GetElement gets the definition for the current element given a workflow state.
func (s *Nats) GetElement(ctx context.Context, state *model.WorkflowState) (*model.Element, error) {
	wf := &model.Workflow{}
	if err := common.LoadObj(ctx, s.wf, state.WorkflowId, wf); errors2.Is(err, nats.ErrKeyNotFound) {
		return nil, fmt.Errorf("load object during get element: %w", err)
	}
	els := common.ElementTable(wf)
	if el, ok := els[state.ElementId]; ok {
		return el, nil
	}
	return nil, fmt.Errorf("get element failed to locate %s: %w", state.ElementId, errors.ErrElementNotFound)
}

func (s *Nats) processTraversals(ctx context.Context) error {
	err := common.Process(ctx, s.js, "traversal", s.closing, subj.NS(messages.WorkflowTraversalExecute, "*"), "Traversal", s.concurrency, func(ctx context.Context, log *slog.Logger, msg *nats.Msg) (bool, error) {
		var traversal model.WorkflowState
		if err := proto.Unmarshal(msg.Data, &traversal); err != nil {
			return false, fmt.Errorf("unmarshal traversal proto: %w", err)
		}

		if _, _, err := s.HasValidProcess(ctx, traversal.ProcessInstanceId, traversal.WorkflowInstanceId); errors2.Is(err, errors.ErrWorkflowInstanceNotFound) || errors2.Is(err, errors.ErrProcessInstanceNotFound) {
			log := logx.FromContext(ctx)
			log.Log(ctx, slog.LevelInfo, "processTraversals aborted due to a missing process")
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
				logx.FromContext(ctx).Error("workflow fatally terminated whilst processing activity", err, slog.String(keys.WorkflowInstanceID, traversal.WorkflowInstanceId), slog.String(keys.WorkflowID, traversal.WorkflowId), err, slog.String(keys.ElementID, traversal.ElementId))
				return true, nil
			} else if err != nil {
				return false, fmt.Errorf("process event: %w", err)
			}
		}
		return true, nil
	})
	if err != nil {
		return fmt.Errorf("traversal processor: %w", err)
	}
	return nil
}

// HasValidProcess - checks for a valid process and instance for a workflow process and instance ids
func (s *Nats) HasValidProcess(ctx context.Context, processInstanceId, workflowInstanceId string) (*model.ProcessInstance, *model.WorkflowInstance, error) {
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

func (s *Nats) hasValidInstance(ctx context.Context, workflowInstanceId string) (*model.WorkflowInstance, error) {
	wfi, err := s.GetWorkflowInstance(ctx, workflowInstanceId)
	if errors2.Is(err, errors.ErrWorkflowInstanceNotFound) {
		return nil, fmt.Errorf("orphaned activity: %w", err)
	}
	if err != nil {
		return nil, err
	}
	return wfi, err
}

func (s *Nats) processTracking(ctx context.Context) error {
	err := common.Process(ctx, s.js, "tracking", s.closing, "WORKFLOW.>", "Tracking", 1, s.track)
	if err != nil {
		return fmt.Errorf("tracking processor: %w", err)
	}
	return nil
}

func (s *Nats) processCompletedJobs(ctx context.Context) error {
	err := common.Process(ctx, s.js, "completedJob", s.closing, subj.NS(messages.WorkFlowJobCompleteAll, "*"), "JobCompleteConsumer", s.concurrency, func(ctx context.Context, log *slog.Logger, msg *nats.Msg) (bool, error) {
		var job model.WorkflowState
		if err := proto.Unmarshal(msg.Data, &job); err != nil {
			return false, fmt.Errorf("unmarshal completed job state: %w", err)
		}
		if _, _, err := s.HasValidProcess(ctx, job.ProcessInstanceId, job.WorkflowInstanceId); errors2.Is(err, errors.ErrWorkflowInstanceNotFound) || errors2.Is(err, errors.ErrProcessInstanceNotFound) {
			log := logx.FromContext(ctx)
			log.Log(ctx, slog.LevelInfo, "processCompletedJobs aborted due to a missing process")
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
		return fmt.Errorf("completed job processor: %w", err)
	}
	return nil
}

func (s *Nats) track(ctx context.Context, log *slog.Logger, msg *nats.Msg) (bool, error) {
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
			return false, fmt.Errorf("save tracking information: %w", err)
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
			return false, fmt.Errorf("delete workflow instance upon completion: %w", err)
		}
	default:

	}
	return true, nil
}

// Conn returns the active nats connection
func (s *Nats) Conn() common.NatsConn { //nolint:ireturn
	return s.conn
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
func (s *Nats) Shutdown() {
	close(s.closing)
}

func (s *Nats) processWorkflowEvents(ctx context.Context) error {
	err := common.Process(ctx, s.js, "workflowEvent", s.closing, subj.NS(messages.WorkflowInstanceAll, "*"), "WorkflowConsumer", s.concurrency, func(ctx context.Context, log *slog.Logger, msg *nats.Msg) (bool, error) {
		var job model.WorkflowState
		if err := proto.Unmarshal(msg.Data, &job); err != nil {
			return false, fmt.Errorf("load workflow state processing workflow event: %w", err)
		}
		if strings.HasSuffix(msg.Subject, ".State.Workflow.Complete") {
			if _, err := s.hasValidInstance(ctx, job.WorkflowInstanceId); errors2.Is(err, errors.ErrWorkflowInstanceNotFound) || errors2.Is(err, errors.ErrProcessInstanceNotFound) {
				log := logx.FromContext(ctx)
				log.Log(ctx, slog.LevelInfo, "processWorkflowEvents aborted due to a missing process")
				return true, nil
			} else if err != nil {
				return false, err
			}
			if err := s.XDestroyWorkflowInstance(ctx, &job); err != nil {
				return false, fmt.Errorf("destroy workflow instance whilst processing workflow events: %w", err)
			}
		}
		return true, nil
	})
	if err != nil {
		return fmt.Errorf("starting workflow event processing: %w", err)
	}
	return nil
}

func (s *Nats) processActivities(ctx context.Context) error {
	err := common.Process(ctx, s.js, "activity", s.closing, subj.NS(messages.WorkflowActivityAll, "*"), "ActivityConsumer", s.concurrency, func(ctx context.Context, log *slog.Logger, msg *nats.Msg) (bool, error) {
		var activity model.WorkflowState
		switch {
		case strings.HasSuffix(msg.Subject, ".State.Activity.Execute"):

		case strings.HasSuffix(msg.Subject, ".State.Activity.Complete"):
			if err := proto.Unmarshal(msg.Data, &activity); err != nil {
				return false, fmt.Errorf("unmarshal state activity complete: %w", err)
			}

			if _, _, err := s.HasValidProcess(ctx, activity.ProcessInstanceId, activity.WorkflowInstanceId); errors2.Is(err, errors.ErrWorkflowInstanceNotFound) || errors2.Is(err, errors.ErrProcessInstanceNotFound) {
				log := logx.FromContext(ctx)
				log.Log(ctx, slog.LevelInfo, "processActivities aborted due to a missing process")
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
				return true, fmt.Errorf("delete saved state upon activity completion: %w", err)
			}
		}

		return true, nil
	})
	if err != nil {
		return fmt.Errorf("starting activity processing: %w", err)
	}
	return nil
}

func (s *Nats) deleteSavedState(activityID string) error {
	if err := common.Delete(s.wfVarState, activityID); err != nil {
		return fmt.Errorf("delete saved state: %w", err)
	}
	return nil
}

// CloseUserTask removes a completed user task.
func (s *Nats) CloseUserTask(ctx context.Context, trackingID string) error {
	job := &model.WorkflowState{}
	if err := common.LoadObj(ctx, s.job, trackingID, job); err != nil {
		return fmt.Errorf("load job when closing user task: %w", err)
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

func (s *Nats) openUserTask(ctx context.Context, owner string, id string) error {
	if err := common.UpdateObj(ctx, s.wfUserTasks, owner, &model.UserTasks{}, func(msg *model.UserTasks) (*model.UserTasks, error) {
		msg.Id = append(msg.Id, id)
		return msg, nil
	}); err != nil {
		return fmt.Errorf("update user task object: %w", err)
	}
	return nil
}

// GetUserTaskIDs gets a list of tasks given an owner.
func (s *Nats) GetUserTaskIDs(ctx context.Context, owner string) (*model.UserTasks, error) {
	ut := &model.UserTasks{}
	if err := common.LoadObj(ctx, s.wfUserTasks, owner, ut); err != nil {
		return nil, fmt.Errorf("load user task IDs: %w", err)
	}
	return ut, nil
}

// OwnerID gets a unique identifier for a task owner.
func (s *Nats) OwnerID(name string) (string, error) {
	if name == "" {
		name = "AnyUser"
	}
	nm, err := s.ownerID.Get(name)
	if err != nil && err != nats.ErrKeyNotFound {
		return "", fmt.Errorf("get owner id: %w", err)
	}
	if nm == nil {
		id := ksuid.New().String()
		if _, err := s.ownerID.Put(name, []byte(id)); err != nil {
			return "", fmt.Errorf("write owner ID: %w", err)
		}
		if _, err = s.ownerName.Put(id, []byte(name)); err != nil {
			return "", fmt.Errorf("store owner name in kv: %w", err)
		}
		return id, nil
	}
	return string(nm.Value()), nil
}

// OwnerName retrieves an owner name given an ID.
func (s *Nats) OwnerName(id string) (string, error) {
	nm, err := s.ownerName.Get(id)
	if err != nil {
		return "", fmt.Errorf("get owner name for id: %w", err)
	}
	return string(nm.Value()), nil
}

func (s *Nats) incrementWorkflowCount() {
	s.statsMx.Lock()
	s.workflowStats.Workflows++
	s.statsMx.Unlock()
}

func (s *Nats) incrementWorkflowCompleted() {
	s.statsMx.Lock()
	s.workflowStats.InstancesComplete++
	s.statsMx.Unlock()
}

func (s *Nats) incrementWorkflowStarted() {
	s.statsMx.Lock()
	s.workflowStats.InstancesStarted++
	s.statsMx.Unlock()
}

func (s *Nats) expectPossibleMissingKey(ctx context.Context, msg string, err error) error {
	if errors2.Is(err, nats.ErrKeyNotFound) {
		log := logx.FromContext(ctx)
		log.Debug(msg, err)
		return nil
	}
	return fmt.Errorf("error: %w", err)
}

// GetOldState gets a task state given its tracking ID.
func (s *Nats) GetOldState(ctx context.Context, id string) (*model.WorkflowState, error) {
	oldState := &model.WorkflowState{}
	err := common.LoadObj(ctx, s.wfVarState, id, oldState)
	if err == nil {
		return oldState, nil
	} else if errors2.Is(err, nats.ErrKeyNotFound) {
		return nil, fmt.Errorf("get old state failed to load object: %w", errors.ErrStateNotFound)
	}
	return nil, fmt.Errorf("retrieving task state: %w", err)
}

func (s *Nats) processLaunch(ctx context.Context) error {
	err := common.Process(ctx, s.js, "launch", s.closing, subj.NS(messages.WorkflowJobLaunchExecute, "*"), "LaunchConsumer", s.concurrency, func(ctx context.Context, log *slog.Logger, msg *nats.Msg) (bool, error) {
		var job model.WorkflowState
		if err := proto.Unmarshal(msg.Data, &job); err != nil {
			return false, fmt.Errorf("unmarshal during process launch: %w", err)
		}
		if _, _, err := s.HasValidProcess(ctx, job.ProcessInstanceId, job.WorkflowInstanceId); errors2.Is(err, errors.ErrWorkflowInstanceNotFound) || errors2.Is(err, errors.ErrProcessInstanceNotFound) {
			log := logx.FromContext(ctx)
			log.Log(ctx, slog.LevelInfo, "processLaunch aborted due to a missing process")
			return true, err
		} else if err != nil {
			return false, err
		}
		if err := s.launchFunc(ctx, &job); err != nil {
			return false, fmt.Errorf("execute launch function: %w", err)
		}
		return true, nil
	})
	if err != nil {
		return fmt.Errorf("start process launch processor: %w", err)
	}
	return nil
}

func (s *Nats) processJobAbort(ctx context.Context) error {
	err := common.Process(ctx, s.js, "abort", s.closing, subj.NS(messages.WorkFlowJobAbortAll, "*"), "JobAbortConsumer", s.concurrency, func(ctx context.Context, log *slog.Logger, msg *nats.Msg) (bool, error) {
		var state model.WorkflowState
		if err := proto.Unmarshal(msg.Data, &state); err != nil {
			return false, fmt.Errorf("job abort consumer failed to unmarshal state: %w", err)
		}
		if _, _, err := s.HasValidProcess(ctx, state.ProcessInstanceId, state.WorkflowInstanceId); errors2.Is(err, errors.ErrWorkflowInstanceNotFound) || errors2.Is(err, errors.ErrProcessInstanceNotFound) {
			log := logx.FromContext(ctx)
			log.Log(ctx, slog.LevelInfo, "processJobAbort aborted due to a missing process")
			return true, err
		} else if err != nil {
			return false, err
		}
		//TODO: Make these idempotently work given missing values
		switch {
		case strings.Contains(msg.Subject, ".State.Job.Abort.ServiceTask"), strings.Contains(msg.Subject, ".State.Job.Abort.Gateway"):
			if err := s.deleteJob(ctx, &state); err != nil {
				return false, fmt.Errorf("delete job during service task abort: %w", err)
			}
		default:
			return true, nil
		}
		return true, nil
	})
	if err != nil {
		return fmt.Errorf("start job abort processor: %w", err)
	}
	return nil
}

func (s *Nats) processProcessComplete(ctx context.Context) error {
	err := common.Process(ctx, s.js, "processComplete", s.closing, subj.NS(messages.WorkflowProcessComplete, "*"), "ProcessCompleteConsumer", s.concurrency, func(ctx context.Context, log *slog.Logger, msg *nats.Msg) (bool, error) {
		var state model.WorkflowState
		if err := proto.Unmarshal(msg.Data, &state); err != nil {
			return false, fmt.Errorf("unmarshal during general abort processor: %w", err)
		}
		pi, wi, err := s.HasValidProcess(ctx, state.ProcessInstanceId, state.WorkflowInstanceId)
		if errors2.Is(err, errors.ErrWorkflowInstanceNotFound) || errors2.Is(err, errors.ErrProcessInstanceNotFound) {
			log := logx.FromContext(ctx)
			log.Log(ctx, slog.LevelInfo, "processProcessComplete aborted due to a missing process")
			return true, err
		} else if err != nil {
			return false, err
		}
		state.State = model.CancellationState_completed
		if err := s.DestroyProcessInstance(ctx, &state, pi, wi); err != nil {
			return false, fmt.Errorf("delete prcess: %w", err)
		}
		return true, nil
	})
	if err != nil {
		return fmt.Errorf("start general abort processor: %w", err)
	}
	return nil

}

func (s *Nats) processGeneralAbort(ctx context.Context) error {
	err := common.Process(ctx, s.js, "abort", s.closing, subj.NS(messages.WorkflowGeneralAbortAll, "*"), "GeneralAbortConsumer", s.concurrency, func(ctx context.Context, log *slog.Logger, msg *nats.Msg) (bool, error) {
		var state model.WorkflowState
		if err := proto.Unmarshal(msg.Data, &state); err != nil {
			return false, fmt.Errorf("unmarshal during general abort processor: %w", err)
		}
		//TODO: Make these idempotently work given missing values
		switch {
		case strings.HasSuffix(msg.Subject, ".State.Activity.Abort"):
			if err := s.deleteActivity(&state); err != nil {
				return false, fmt.Errorf("delete activity during general abort processor: %w", err)
			}
		case strings.HasSuffix(msg.Subject, ".State.Workflow.Abort"):
			abortState := common.CopyWorkflowState(&state)
			abortState.State = model.CancellationState_terminated
			if err := s.XDestroyWorkflowInstance(ctx, &state); err != nil {
				return false, fmt.Errorf("delete workflow during general abort processor: %w", err)
			}
		default:
			return true, nil
		}
		return true, nil
	})
	if err != nil {
		return fmt.Errorf("start general abort processor: %w", err)
	}
	return nil
}

func (s *Nats) deleteActivity(state *model.WorkflowState) error {
	if err := s.deleteSavedState(common.TrackingID(state.Id).ID()); err != nil && !errors2.Is(err, nats.ErrKeyNotFound) {
		return fmt.Errorf("delete activity: %w", err)
	}
	return nil
}

func (s *Nats) deleteJob(ctx context.Context, state *model.WorkflowState) error {
	if err := s.DeleteJob(ctx, common.TrackingID(state.Id).ID()); err != nil && !errors2.Is(err, nats.ErrKeyNotFound) {
		return fmt.Errorf("delete job: %w", err)
	}
	if activityState, err := s.GetOldState(ctx, common.TrackingID(state.Id).Pop().ID()); err != nil && !errors2.Is(err, errors.ErrStateNotFound) {
		return fmt.Errorf("fetch old state during delete job: %w", err)
	} else if err == nil {
		if err := s.PublishWorkflowState(ctx, subj.NS(messages.WorkflowActivityAbort, "default"), activityState); err != nil {
			return fmt.Errorf("publish activity abort during delete job: %w", err)
		}
	}
	return nil
}

// SaveState saves the task state.
func (s *Nats) SaveState(ctx context.Context, id string, state *model.WorkflowState) error {
	saveState := proto.Clone(state).(*model.WorkflowState)
	saveState.Id = common.TrackingID(saveState.Id).Pop().Push(id)
	data, err := proto.Marshal(saveState)
	if err != nil {
		return fmt.Errorf("unmarshal saved state: %w", err)
	}
	if err := common.Save(ctx, s.wfVarState, id, data); err != nil {
		return fmt.Errorf("save state: %w", err)
	}
	return nil
}

// CreateProcessInstance creates a new instance of a process and attaches it to the workflow instance.
func (s *Nats) CreateProcessInstance(ctx context.Context, workflowInstanceID string, parentProcessID string, parentElementID string, processName string) (*model.ProcessInstance, error) {
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
func (s *Nats) GetProcessInstance(ctx context.Context, processInstanceID string) (*model.ProcessInstance, error) {
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
func (s *Nats) DestroyProcessInstance(ctx context.Context, state *model.WorkflowState, pi *model.ProcessInstance, wi *model.WorkflowInstance) error {
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
	if err := s.PublishWorkflowState(ctx, messages.WorkflowProcessTerminated, state); err != nil {
		return fmt.Errorf("destroy process instance failed initiaite completing workflow instance: %w", err)
	}
	if len(wfi.ProcessInstanceId) == 0 && !lock {
		if err := s.PublishWorkflowState(ctx, messages.WorkflowInstanceComplete, state); err != nil {
			return fmt.Errorf("destroy process instance failed initiaite completing workflow instance: %w", err)
		}
	}
	return nil
}

func (s *Nats) populateMetadata(wf *model.Workflow) {
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
func (s *Nats) SatisfyProcess(ctx context.Context, workflowInstance *model.WorkflowInstance, processName string) error {
	err := common.UpdateObj(ctx, s.wfInstance, workflowInstance.WorkflowInstanceId, workflowInstance, func(wi *model.WorkflowInstance) (*model.WorkflowInstance, error) {
		wi.SatisfiedProcesses[processName] = true
		return wi, nil
	})
	if err != nil {
		return fmt.Errorf("satify process: %w", err)
	}
	return nil
}
