package workflow

import (
	"bytes"
	"context"
	"encoding/gob"
	"fmt"
	"github.com/antonmedv/expr"
	"github.com/crystal-construct/shar/model"
	"github.com/crystal-construct/shar/server/errors"
	"github.com/crystal-construct/shar/server/errors/keys"
	"github.com/crystal-construct/shar/server/messages"
	"github.com/crystal-construct/shar/server/services"
	"github.com/nats-io/nats.go"
	"github.com/segmentio/ksuid"
	"go.uber.org/zap"
	"sync"
)

// Engine contains the workflow processing functions
type Engine struct {
	con     *nats.Conn
	js      nats.JetStreamContext
	log     *zap.Logger
	closing chan struct{}
	closed  chan struct{}
	mx      sync.Mutex
	ns      *services.NatsService
}

// NewEngine returns an instance of the core workflow engine.
func NewEngine(log *zap.Logger, ns *services.NatsService) (*Engine, error) {
	e := &Engine{
		ns:      ns,
		closing: make(chan struct{}),
		log:     log,
	}
	return e, nil
}

// Start sets up the activity and job processors and starts the engine processing workflows.
func (c *Engine) Start(ctx context.Context) error {
	c.ns.SetEventProcessor(c.activityProcessor)
	c.ns.SetCompleteJobProcessor(c.completeJobProcessor)
	c.ns.SetMessageCompleteProcessor(c.messageCompleteProcessor)
	return c.ns.StartProcessing(ctx)
}

// LoadWorkflow loads a model.Process describing a workflow into the engine ready for execution.
func (c *Engine) LoadWorkflow(ctx context.Context, model *model.Workflow) (string, error) {
	wfId, err := c.ns.StoreWorkflow(ctx, model)
	if err != nil {
		return "", err
	}
	return wfId, nil
}

// Launch starts a new instance of a workflow and returns a workflow instance Id.
func (c *Engine) Launch(ctx context.Context, workflowName string, vars []byte) (string, error) {
	return c.launch(ctx, workflowName, vars, "", "")
}

// launch contains the underlying logic to start a workflow.  It is also called to spawn new instances of child workflows.
func (c *Engine) launch(ctx context.Context, workflowName string, vars []byte, parentWfiID string, parentElID string) (string, error) {
	select {
	case <-c.closing:
		return "", errors.ErrClosing
	default:
	}

	wfId, err := c.ns.GetLatestVersion(ctx, workflowName)
	if err != nil {
		return "", c.engineErr(ctx, "failed to get latest version of workflow", err,
			zap.String(keys.ParentInstanceElementId, parentElID),
			zap.String(keys.ParentWorkflowInstanceId, parentWfiID),
			zap.String(keys.WorkflowName, workflowName),
		)
	}

	wf, err := c.ns.GetWorkflow(ctx, wfId)
	if err != nil {
		return "", c.engineErr(ctx, "failed to get workflow", err,
			zap.String(keys.ParentInstanceElementId, parentElID),
			zap.String(keys.ParentWorkflowInstanceId, parentWfiID),
			zap.String(keys.WorkflowName, workflowName),
			zap.String(keys.WorkflowID, wfId),
		)
	}
	wfi, err := c.ns.CreateWorkflowInstance(ctx, &model.WorkflowInstance{WorkflowId: wfId, ParentWorkflowInstanceId: &parentWfiID, ParentElementId: &parentElID})
	if err != nil {
		return "", c.engineErr(ctx, "failed to create workflow instance", err,
			zap.String(keys.ParentInstanceElementId, parentElID),
			zap.String(keys.ParentWorkflowInstanceId, parentWfiID),
			zap.String(keys.WorkflowName, workflowName),
			zap.String(keys.WorkflowID, wfId),
		)
	}
	els := elementTable(wf)
	errs := make(chan error)
	wg := sync.WaitGroup{}
	forEachStartElement(wf, func(el *model.Element) {
		wg.Add(1)

		if err := c.ns.PublishWorkflowState(ctx, messages.WorkflowInstanceExecute, &model.WorkflowState{
			WorkflowInstanceId: wfi.WorkflowInstanceId,
			WorkflowId:         wfId,
			ElementId:          el.Id,
			ElementType:        el.Type,
			Vars:               nil,
		}); err != nil {
			errs <- c.engineErr(ctx, "failed to publish workflow state", err,
				zap.String(keys.ParentInstanceElementId, parentElID),
				zap.String(keys.ParentWorkflowInstanceId, parentWfiID),
				zap.String(keys.WorkflowName, workflowName),
				zap.String(keys.WorkflowID, wfId),
			)
		}
		go func(el *model.Element) {
			defer wg.Done()

			if err := c.traverse(ctx, wfi, el.Outbound, els, vars); err != nil {
				errs <- fmt.Errorf("failed traversal to %v: %w", el.Outbound, err)
			}

		}(el)
	})
	wg.Wait()
	close(errs)
	if err := <-errs; err != nil {
		return "", c.engineErr(ctx, "failed initial traversal", err,
			zap.String(keys.ParentInstanceElementId, parentElID),
			zap.String(keys.ParentWorkflowInstanceId, parentWfiID),
			zap.String(keys.WorkflowName, workflowName),
			zap.String(keys.WorkflowID, wfId),
		)
	}
	return wfi.WorkflowInstanceId, nil
}

// encodeVars encodes the map of workflow variables into a go binary to be sent across the wire.
func (c *Engine) encodeVars(ctx context.Context, vars model.Vars) []byte {
	var buf bytes.Buffer
	enc := gob.NewEncoder(&buf)
	if err := enc.Encode(vars); err != nil {
		c.log.Error("failed to encode vars", zap.Any("vars", vars))
	}
	return buf.Bytes()
}

// decodeVars decodes a go binary object containing workflow variables.
func (c *Engine) decodeVars(ctx context.Context, vars []byte) model.Vars {
	ret := make(map[string]any)
	if vars == nil {
		return ret
	}
	r := bytes.NewReader(vars)
	d := gob.NewDecoder(r)
	if err := d.Decode(&ret); err != nil {
		c.log.Error("failed to decode vars", zap.Any("vars", vars))
	}
	return ret
}

// forEachStartElement finds all start elements for a given process and executes a function on the element.
func forEachStartElement(wf *model.Workflow, fn func(element *model.Element)) {
	for _, pr := range wf.Process {
		for _, i := range pr.Elements {
			if i.Type == "startEvent" {
				fn(i)
			}
		}
	}
}

// traverse traverses all outbound connections provided the conditions passed if available.
func (c *Engine) traverse(ctx context.Context, wfi *model.WorkflowInstance, outbound *model.Targets, el map[string]*model.Element, vars []byte) error {
	if outbound == nil {
		return nil
	}
	// Traverse along all outbound edges
	for _, t := range outbound.Target {
		ok := true
		// Evaluate conditions
		for _, ex := range t.Conditions {

			// TODO: Cache compilation.
			exVars := c.decodeVars(ctx, vars)
			program, err := expr.Compile(ex, expr.Env(exVars))
			if err != nil {

				return err
			}
			res, err := expr.Run(program, exVars)
			if err != nil {

				return err
			}
			if !res.(bool) {
				ok = false

				break
			}

		}

		trackingId := ksuid.New().String()
		// If the conditions passed commit a traversal
		if ok {
			if err := c.ns.PublishWorkflowState(ctx, messages.WorkflowTraversalExecute, &model.WorkflowState{
				ElementType:        el[t.Target].Type,
				ElementId:          t.Target,
				WorkflowId:         wfi.WorkflowId,
				WorkflowInstanceId: wfi.WorkflowInstanceId,
				TrackingId:         trackingId,
				Vars:               vars,
			}); err != nil {
				c.log.Error("failed to publish workflow state", zap.Error(err))
				return err
			}
			//if err := c.queue.Traverse(ctx, wfi.WorkflowInstanceId, t.Target, vars); err != nil {
			//	c.log.Ctx(ctx).Error("failed to traverse to "+el[t.Target].Name, zap.Error(err))
			//	return err
			//}
			if outbound.Exclusive {
				break
			}
		}
	}
	return nil
}

func (c *Engine) activityProcessor(ctx context.Context, wfiId, elementId, trackingId string, vars []byte) error {
	select {
	case <-c.closing:
		return errors.ErrClosing
	default:
	}

	wfi, err := c.ns.GetWorkflowInstance(ctx, wfiId)
	if err == errors.ErrWorkflowInstanceNotFound {

		c.log.Warn("workflow instance not found, cancelling activity", zap.Error(err), zap.String(keys.WorkflowInstanceID, wfiId))
		return nil
	} else if err != nil {
		return c.engineErr(ctx, "failed to get workflow instance", err,
			zap.String(keys.WorkflowInstanceID, wfi.WorkflowInstanceId),
		)
	}
	process, err := c.ns.GetWorkflow(ctx, wfi.WorkflowId)
	if err != nil {
		return c.engineErr(ctx, "failed to get workflow", err,
			zap.String(keys.WorkflowInstanceID, wfi.WorkflowInstanceId),
			zap.String(keys.WorkflowID, wfi.WorkflowId),
		)
	}
	els := elementTable(process)
	el := els[elementId]
	if err := c.ns.PublishWorkflowState(ctx, messages.WorkflowActivityExecute, &model.WorkflowState{
		ElementType:        el.Type,
		ElementId:          elementId,
		WorkflowInstanceId: wfiId,
		WorkflowId:         wfi.WorkflowId,
		Vars:               vars,
	}); err != nil {
		return c.engineErr(ctx, "failed to publish workflow state", err, apErrFields(wfi.WorkflowInstanceId, wfi.WorkflowId, el.Id, el.Name, el.Type, process.Name)...)
	}
	if err := c.ns.PublishWorkflowState(ctx, messages.WorkflowTraversalComplete, &model.WorkflowState{
		ElementType:        el.Type,
		ElementId:          elementId,
		WorkflowId:         wfi.WorkflowId,
		WorkflowInstanceId: wfiId,
		TrackingId:         trackingId,
		Vars:               vars,
	}); err != nil {
		return c.engineErr(ctx, "failed to publish workflow state", err, apErrFields(wfi.WorkflowInstanceId, wfi.WorkflowId, el.Id, el.Name, el.Type, process.Name)...)
	}

	var workflowComplete bool

	switch el.Type {
	case "serviceTask":
		if err := c.startJob(ctx, messages.WorkflowJobServiceTaskExecute, wfi.WorkflowId, wfiId, el, "", vars); err != nil {
			return c.engineErr(ctx, "failed to start job", err, apErrFields(wfi.WorkflowInstanceId, wfi.WorkflowId, el.Id, el.Name, el.Type, process.Name)...)
		}
	case "userTask":
		if err := c.startJob(ctx, messages.WorkflowJobUserTaskExecute, wfi.WorkflowId, wfiId, el, "", vars); err != nil {
			return c.engineErr(ctx, "failed to start job", err, apErrFields(wfi.WorkflowInstanceId, wfi.WorkflowId, el.Id, el.Name, el.Type, process.Name)...)
		}
	case "manualTask":
		if err := c.startJob(ctx, messages.WorkflowJobManualTaskExecute, wfi.WorkflowId, wfiId, el, "", vars); err != nil {
			return c.engineErr(ctx, "failed to start job", err, apErrFields(wfi.WorkflowInstanceId, wfi.WorkflowId, el.Id, el.Name, el.Type, process.Name)...)
		}
	case "intermediateThrowEvent":
		wf, err := c.ns.GetWorkflow(ctx, wfi.WorkflowId)
		if err != nil {
			return err
		}
		ix := -1
		for i, v := range wf.Messages {
			if v.Name == el.Execute {
				ix = i
				break
			}
		}
		if ix == -1 {
			// TODO: Fatal workflow error - we shouldn't allow to send unknown messages in parser
			return fmt.Errorf("unknown workflow message name: %s", el.Execute)
		}
		if err := c.startJob(ctx, messages.WorkflowJobSendMessageExecute, wfi.WorkflowId, wfiId, el, wf.Messages[ix].Execute, vars); err != nil {
			return c.engineErr(ctx, "failed to start job", err, apErrFields(wfi.WorkflowInstanceId, wfi.WorkflowId, el.Id, el.Name, el.Type, process.Name)...)
		}
	case "callActivity":
		if _, err := c.launch(ctx, el.Execute, vars, wfiId, el.Id); err != nil {
			return c.engineErr(ctx, "failed to launch child workflow", err, apErrFields(wfi.WorkflowInstanceId, wfi.WorkflowId, el.Id, el.Name, el.Type, process.Name)...)
		}
	case "intermediateCatchEvent":
		if err := c.awaitMessage(ctx, wfi.WorkflowId, wfiId, el, vars); err != nil {
			return c.engineErr(ctx, "failed to await message", err, apErrFields(wfi.WorkflowInstanceId, wfi.WorkflowId, el.Id, el.Name, el.Type, process.Name)...)
		}
	case "endEvent":
		if *wfi.ParentWorkflowInstanceId != "" {
			if err := c.returnBack(ctx, wfi.WorkflowInstanceId, *wfi.ParentWorkflowInstanceId, *wfi.ParentElementId, vars); err != nil {
				return c.engineErr(ctx, "failed to return to originator workflow", err, apErrFields(wfi.WorkflowInstanceId, wfi.WorkflowId, el.Id, el.Name, el.Type, process.Name, zap.String(keys.ParentWorkflowInstanceId, *wfi.ParentWorkflowInstanceId))...)
			}
		}
		if err := c.cleanup(ctx, wfi.WorkflowInstanceId); err != nil {
			return c.engineErr(ctx, "failed to return to remove workflow instance", err, apErrFields(wfi.WorkflowInstanceId, wfi.WorkflowId, el.Id, el.Name, el.Type, process.Name)...)
		}
		workflowComplete = true
	default:
		if err := c.traverse(ctx, wfi, el.Outbound, els, vars); err != nil {
			return c.engineErr(ctx, "failed to return to traverse", err, apErrFields(wfi.WorkflowInstanceId, wfi.WorkflowId, el.Id, el.Name, el.Type, process.Name)...)
		}
	}
	if err := c.ns.PublishWorkflowState(ctx, messages.WorkflowActivityComplete, &model.WorkflowState{
		ElementType:        el.Type,
		ElementId:          elementId,
		WorkflowId:         wfi.WorkflowId,
		WorkflowInstanceId: wfiId,
		Vars:               vars,
	}); err != nil {
		return c.engineErr(ctx, "failed to publish workflow state", err, apErrFields(wfi.WorkflowInstanceId, wfi.WorkflowId, el.Id, el.Name, el.Type, process.Name)...)
	}

	if workflowComplete {
		if err := c.ns.PublishWorkflowState(ctx, messages.WorkflowInstanceComplete, &model.WorkflowState{
			ElementType:        el.Type,
			ElementId:          elementId,
			WorkflowId:         wfi.WorkflowId,
			WorkflowInstanceId: wfiId,
			Vars:               vars,
		}); err != nil {
			return c.engineErr(ctx, "failed to publish workflow state", err, apErrFields(wfi.WorkflowInstanceId, wfi.WorkflowId, el.Id, el.Name, el.Type, process.Name)...)
		}
	}
	return nil
}

func apErrFields(workflowInstanceID, workflowID, elementID, elementName, elementType, workflowName string, extraFields ...zap.Field) []zap.Field {
	fields := []zap.Field{
		zap.String(keys.WorkflowInstanceID, workflowInstanceID),
		zap.String(keys.WorkflowID, workflowID),
		zap.String(keys.ElementID, elementID),
		zap.String(keys.ElementName, elementName),
		zap.String(keys.ElementType, elementType),
		zap.String(keys.WorkflowName, workflowName),
	}
	if len(extraFields) > 0 {
		fields = append(fields, extraFields...)
	}
	return fields
}

func (c *Engine) returnBack(ctx context.Context, wfiID string, parentWfiID string, parentElID string, vars []byte) error {
	pwfi, err := c.ns.GetWorkflowInstance(ctx, parentWfiID)
	if err == errors.ErrWorkflowInstanceNotFound {
		c.log.Warn("parent workflow instance not found, cancelling return to caller", zap.Error(err), zap.String(keys.ParentWorkflowInstanceId, parentWfiID))
		return nil
	} else if err != nil {
		return fmt.Errorf("failed to fetch workflow instance for return back: %w", err)
	}
	pwf, err := c.ns.GetWorkflow(ctx, pwfi.WorkflowId)
	if err != nil {
		return c.engineErr(ctx, "failed to fetch return workflow", err,
			zap.String(keys.ParentWorkflowInstanceId, parentWfiID),
			zap.String(keys.WorkflowInstanceID, wfiID),
			zap.String(keys.ElementID, parentElID),
		)
	}
	index := elementTable(pwf)
	el := index[parentElID]
	err = c.traverse(ctx, pwfi, el.Outbound, index, vars)
	if err != nil {
		return c.engineErr(ctx, "failed to traverse", err,
			zap.String(keys.ElementName, el.Name),
			zap.String(keys.ElementID, el.Id),
			zap.String(keys.ElementType, el.Type),
			zap.String(keys.WorkflowInstanceID, wfiID),
			zap.String(keys.ParentWorkflowInstanceId, parentWfiID),
		)
	}
	return nil
}

func (c *Engine) cleanup(ctx context.Context, wfiId string) error {
	if err := c.ns.DestroyWorkflowInstance(ctx, wfiId); err != nil {
		return fmt.Errorf("failed to destroy workflow instance: %w", err)
	}
	return nil
}

func (c *Engine) completeJobProcessor(ctx context.Context, jobId string, vars []byte) error {
	select {
	case <-c.closing:
		return errors.ErrClosing
	default:
	}

	job, err := c.ns.GetJob(ctx, jobId)
	if err != nil {
		return c.engineErr(ctx, "failed to locate job", err,
			zap.String(keys.JobID, jobId),
		)
	}

	wfi, err := c.ns.GetWorkflowInstance(ctx, job.WorkflowInstanceId)
	if err == errors.ErrWorkflowInstanceNotFound {
		c.log.Warn("workflow instance not found, cancelling job processing", zap.Error(err), zap.String(keys.WorkflowInstanceID, job.WorkflowInstanceId))
		return nil
	} else if err != nil {
		return c.engineErr(ctx, "failed to get workflow instance for job", err,
			zap.String(keys.JobType, job.ElementType),
			zap.String(keys.JobID, jobId),
		)
	}

	wf, err := c.ns.GetWorkflow(ctx, wfi.WorkflowId)
	if err != nil {
		return c.engineErr(ctx, "failed to fetch job workflow", err,
			zap.String(keys.JobType, job.ElementType),
			zap.String(keys.JobID, jobId),
			zap.String(keys.WorkflowInstanceID, wfi.WorkflowInstanceId),
			zap.String(keys.WorkflowID, wfi.WorkflowId),
		)
	}
	els := elementTable(wf)
	el := els[job.ElementId]
	if err := c.traverse(ctx, wfi, el.Outbound, els, vars); err != nil {
		return c.engineErr(ctx, "failed to launch traversal", err,
			zap.String(keys.ElementName, el.Name),
			zap.String(keys.ElementID, el.Id),
			zap.String(keys.ElementType, el.Type),
			zap.String(keys.JobType, job.ElementType),
			zap.String(keys.JobID, jobId),
			zap.String(keys.WorkflowInstanceID, wfi.WorkflowInstanceId),
			zap.String(keys.WorkflowID, wfi.WorkflowId),
		)
	}
	return nil
}

func (c *Engine) startJob(ctx context.Context, subject string, wfId string, wfiId string, el *model.Element, condition string, vars []byte) error {
	job := &model.WorkflowState{WorkflowId: wfId, WorkflowInstanceId: wfiId, Vars: vars, ElementType: el.Type, ElementId: el.Id, Execute: &el.Execute, Condition: &condition}
	jobId, err := c.ns.CreateJob(ctx, job)

	if err != nil {
		return c.engineErr(ctx, "failed to start manual task", err,
			zap.String(keys.ElementName, el.Name),
			zap.String(keys.ElementID, el.Id),
			zap.String(keys.ElementType, el.Type),
			zap.String(keys.JobType, job.ElementType),
			zap.String(keys.JobID, jobId),
			zap.String(keys.WorkflowInstanceID, wfiId),
		)
	}
	job.TrackingId = jobId
	return c.ns.PublishJob(ctx, subject, el, job)
}

// elementTable indexes an entire process for quick Id lookups
func elementTable(process *model.Workflow) map[string]*model.Element {
	el := make(map[string]*model.Element)
	for _, i := range process.Process {
		indexProcessElements(i.Elements, el)
	}
	return el
}

// indexProcessElements is the recursive part of the index
func indexProcessElements(elements []*model.Element, el map[string]*model.Element) {
	for _, i := range elements {
		el[i.Id] = i
		if i.Process != nil {
			indexProcessElements(i.Process.Elements, el)
		}
	}
}

func (c *Engine) engineErr(ctx context.Context, msg string, err error, z ...zap.Field) error {
	z = append(z, zap.Error(err))
	c.log.Error(msg, z...)

	return err
}

func (c *Engine) Shutdown() {
	select {
	case <-c.closing:
		return
	default:
		close(c.closing)
		c.ns.Shutdown()
		return
	}
}

func (c *Engine) CancelWorkflowInstance(ctx context.Context, id string) error {
	return c.ns.DestroyWorkflowInstance(ctx, id)
}

func (c *Engine) awaitMessage(ctx context.Context, wfId string, wfiId string, el *model.Element, vars []byte) error {
	trackingId := ksuid.New().String()
	awaitMsg := &model.WorkflowState{
		WorkflowId:         wfId,
		WorkflowInstanceId: wfiId,
		ElementId:          el.Id,
		ElementType:        el.Type,
		TrackingId:         trackingId,
		Execute:            &el.Execute,
		Condition:          &el.Msg,
		Vars:               vars,
	}
	err := c.ns.AwaitMsg(ctx, el.Msg, awaitMsg)

	if err != nil {
		return c.engineErr(ctx, "failed to await message", err,
			zap.String(keys.ElementName, el.Name),
			zap.String(keys.ElementID, el.Id),
			zap.String(keys.ElementType, el.Type),
			zap.String(keys.JobType, awaitMsg.ElementType),
			zap.String(keys.WorkflowInstanceID, wfiId),
			zap.String(keys.Execute, *awaitMsg.Execute),
		)
	}
	return nil
}

func (c *Engine) messageCompleteProcessor(ctx context.Context, state *model.WorkflowState) error {
	wfi, err := c.ns.GetWorkflowInstance(ctx, state.WorkflowInstanceId)
	if err == nats.ErrKeyNotFound {
		c.log.Warn("workflow instance not found, cancelling message processing", zap.Error(err), zap.String(keys.WorkflowInstanceID, state.WorkflowInstanceId))
		return nil
	} else if err != nil {
		return err
	}
	wf, err := c.ns.GetWorkflow(ctx, state.WorkflowId)
	if err != nil {
		return err
	}
	els := elementTable(wf)
	return c.traverse(ctx, wfi, els[state.ElementId].Outbound, els, state.Vars)
}
