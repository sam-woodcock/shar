package workflow

import (
	"bytes"
	"context"
	"encoding/gob"
	"fmt"
	"github.com/antonmedv/expr"
	"github.com/crystal-construct/shar/internal/messages"
	"github.com/crystal-construct/shar/model"
	"github.com/crystal-construct/shar/server/errors"
	"github.com/crystal-construct/shar/server/services"
	"github.com/crystal-construct/shar/telemetry/keys"
	"github.com/nats-io/nats.go"
	"github.com/uptrace/opentelemetry-go-extra/otelzap"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"golang.org/x/text/cases"
	"golang.org/x/text/language"
	"sync"
)

// Engine contains the workflow processing functions
type Engine struct {
	con     *nats.Conn
	js      nats.JetStreamContext
	queue   services.Queue
	store   services.Storage
	log     *otelzap.Logger
	closing chan struct{}
	closed  chan struct{}
	mx      sync.Mutex
}

var tracer = otel.Tracer("shar")

// titleCaser provides the ability to transform camel case to pascal case.
var titleCaser = cases.Title(language.English, cases.NoLower)

// NewEngine returns an instance of the core workflow engine.
func NewEngine(log *otelzap.Logger, store services.Storage, queue services.Queue) (*Engine, error) {
	e := &Engine{
		store:   store,
		queue:   queue,
		log:     log,
		closing: make(chan struct{}),
	}
	return e, nil
}

// Start sets up the activity and job processors and starts the engine processing workflows.
func (c *Engine) Start(ctx context.Context) error {
	c.queue.SetEventProcessor(c.activityProcessor)
	c.queue.SetCompleteJobProcessor(c.completeJobProcessor)
	return c.queue.StartProcessing(ctx)
}

// LoadWorkflow loads a model.Process describing a workflow into the engine ready for execution.
func (c *Engine) LoadWorkflow(ctx context.Context, model *model.Process) (string, error) {
	wfId, err := c.store.StoreWorkflow(ctx, model)
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
	wfNameAttr := attribute.KeyValue{Key: keys.WorkflowName, Value: attribute.StringValue(workflowName)}
	parentWfiIDAttr := attribute.KeyValue{Key: keys.ParentWorkflowInstanceId, Value: attribute.StringValue(parentWfiID)}
	parentElIDAttr := attribute.KeyValue{Key: keys.ParentInstanceElementId, Value: attribute.StringValue(parentElID)}

	ctx, span := tracer.Start(ctx, "WorkflowInstanceLaunch", trace.WithAttributes(wfNameAttr, parentWfiIDAttr, parentElIDAttr))
	defer span.End()
	wfId, err := c.store.GetLatestVersion(ctx, workflowName)
	if err != nil {
		return "", c.engineErr(ctx, span, "failed to get latest version of workflow", err, wfNameAttr, parentWfiIDAttr, parentElIDAttr)
	}
	wfIDAttr := attribute.KeyValue{Key: keys.WorkflowID, Value: attribute.StringValue(wfId)}
	span.SetAttributes(wfIDAttr)
	wf, err := c.store.GetWorkflow(ctx, wfId)
	if err != nil {
		return "", c.engineErr(ctx, span, "failed to get workflow", err, wfNameAttr, parentWfiIDAttr, parentElIDAttr, wfIDAttr)
	}
	wfi, err := c.store.CreateWorkflowInstance(ctx, &model.WorkflowInstance{WorkflowId: wfId, ParentWorkflowInstanceId: parentWfiID, ParentElementId: parentElID})
	if err != nil {
		return "", c.engineErr(ctx, span, "failed to create workflow instance", err, wfNameAttr, parentWfiIDAttr, parentElIDAttr, wfIDAttr)
	}
	span.SetAttributes(
		attribute.KeyValue{Key: keys.WorkflowInstanceID, Value: attribute.StringValue(wfi.WorkflowInstanceId)},
		attribute.KeyValue{Key: keys.WorkflowID, Value: attribute.StringValue(wfId)},
	)
	els := elementTable(wf)
	errs := make(chan error)
	wg := sync.WaitGroup{}
	forEachStartElement(wf.Elements, func(el *model.Element) {
		wg.Add(1)
		elNameAttr := attribute.KeyValue{Key: keys.ElementName, Value: attribute.StringValue(el.Name)}
		elIDAttr := attribute.KeyValue{Key: keys.ElementID, Value: attribute.StringValue(el.Id)}
		elTypeAttr := attribute.KeyValue{Key: keys.ElementType, Value: attribute.StringValue(el.Type)}
		ctx, span := tracer.Start(ctx, "ElementStart", trace.WithAttributes(elNameAttr, elIDAttr, elTypeAttr, wfIDAttr))
		if err := c.queue.PublishWorkflowState(ctx, messages.WorkflowInstanceStart, &model.WorkflowState{
			WorkflowInstanceId: wfi.WorkflowInstanceId,
			ElementId:          el.Id,
			ElementType:        el.Type,
			Vars:               nil,
		}); err != nil {
			errs <- c.engineErr(ctx, span, "failed to publish workflow state", err, wfNameAttr, parentWfiIDAttr, parentElIDAttr, wfIDAttr, elNameAttr, elIDAttr, elTypeAttr)
		}
		go func(el *model.Element, span trace.Span) {
			defer wg.Done()
			defer span.End()
			if err := c.traverse(ctx, wfi, el.Outbound, els, vars); err != nil {
				errs <- fmt.Errorf("failed traversal to %v: %w", el.Outbound, err)
			}

		}(el, span)
	})
	wg.Wait()
	close(errs)
	if err := <-errs; err != nil {
		return "", c.engineErr(ctx, span, "failed initial traversal", err, wfNameAttr, parentWfiIDAttr, parentElIDAttr, wfIDAttr)
	}
	return wfi.WorkflowInstanceId, nil
}

// encodeVars encodes the map of workflow variables into a go binary to be sent across the wire.
func (c *Engine) encodeVars(ctx context.Context, vars model.Vars) []byte {
	var buf bytes.Buffer
	enc := gob.NewEncoder(&buf)
	if err := enc.Encode(vars); err != nil {
		c.log.Ctx(ctx).Error("failed to encode vars", zap.Any("vars", vars))
	}
	return buf.Bytes()
}

// decodeVars decodes a go binary object containing workflow variables.
func (c *Engine) decodeVars(ctx context.Context, vars []byte) model.Vars {
	ret := make(map[string]interface{})
	if vars == nil {
		return ret
	}
	r := bytes.NewReader(vars)
	d := gob.NewDecoder(r)
	if err := d.Decode(&ret); err != nil {
		c.log.Ctx(ctx).Error("failed to decode vars", zap.Any("vars", vars))
	}
	return ret
}

// forEachStartElement finds all start elements for a given process and executes a function on the element.
func forEachStartElement(els []*model.Element, fn func(element *model.Element)) {
	for _, i := range els {
		if i.Type == "startEvent" {
			fn(i)
		}
	}
}

// traverse traverses all outbound connections provided the conditions passed if available.
func (c *Engine) traverse(ctx context.Context, wfi *model.WorkflowInstance, outbound *model.Targets, el map[string]*model.Element, vars []byte) error {
	// Traverse along all outbound edges
	for _, t := range outbound.Target {
		ok := true
		// Evaluate conditions
		for _, ex := range t.Conditions {
			ctx, span1 := tracer.Start(ctx, "Evaluate condition "+ex)
			// TODO: Cache compilation.
			exVars := c.decodeVars(ctx, vars)
			program, err := expr.Compile(ex, expr.Env(exVars))
			if err != nil {
				c.log.Ctx(ctx).Error("expression compilation error", zap.Error(err), zap.String("expr", ex))
				span1.End()
				return err
			}
			res, err := expr.Run(program, exVars)
			if err != nil {
				c.log.Ctx(ctx).Error("expression evaluation error", zap.String("expr", ex), zap.Error(err))
				span1.End()
				return err
			}
			if !res.(bool) {
				ok = false
				span1.End()
				break
			}
			span1.End()
		}

		// If the conditions passed commit a traversal
		if ok {
			if err := c.queue.PublishWorkflowState(ctx, messages.WorkflowTraversalExecute, &model.WorkflowState{
				ElementType:        el[t.Target].Type,
				ElementId:          t.Target,
				WorkflowInstanceId: wfi.WorkflowInstanceId,
				Vars:               vars,
			}); err != nil {
				c.log.Ctx(ctx).Error("failed to publish workflow state", zap.Error(err))
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
func (c *Engine) activityProcessor(ctx context.Context, wfiId, elementId string, vars []byte) error {
	select {
	case <-c.closing:
		return errors.ErrClosing
	default:
	}
	ctx, span := tracer.Start(ctx, "ActivityProcessor")
	defer span.End()
	wfiIDAttr := attribute.KeyValue{Key: keys.WorkflowInstanceID, Value: attribute.StringValue(wfiId)}
	span.SetAttributes(wfiIDAttr)
	wfi, err := c.store.GetWorkflowInstance(ctx, wfiId)
	if err == errors.ErrWorkflowInstanceNotFound {
		span.SetStatus(codes.Error, "workflow instance not found, cancelling")
		c.log.Ctx(ctx).Warn("workflow instance not found, cancelling activity", zap.Error(err), zap.String(keys.WorkflowInstanceID, wfiId))
		return nil
	} else if err != nil {
		return c.engineErr(ctx, span, "failed to get workflow instance", err, wfiIDAttr)
	}
	wfIDAttr := attribute.KeyValue{Key: keys.WorkflowID, Value: attribute.StringValue(wfi.WorkflowId)}
	span.SetAttributes(wfIDAttr)
	process, err := c.store.GetWorkflow(ctx, wfi.WorkflowId)
	if err != nil {
		return c.engineErr(ctx, span, "failed to get workflow", err, wfiIDAttr, wfIDAttr)
	}
	els := elementTable(process)
	el := els[elementId]
	elIDAttr := attribute.KeyValue{Key: keys.ElementID, Value: attribute.StringValue(el.Id)}
	elNameAttr := attribute.KeyValue{Key: keys.ElementName, Value: attribute.StringValue(el.Name)}
	elTypeAttr := attribute.KeyValue{Key: keys.ElementType, Value: attribute.StringValue(el.Type)}
	wfNameAttr := attribute.KeyValue{Key: keys.WorkflowName, Value: attribute.StringValue(process.Name)}
	span.SetAttributes(elTypeAttr, elNameAttr, elIDAttr, wfNameAttr)
	if err := c.queue.PublishWorkflowState(ctx, messages.WorkflowActivityExecute, &model.WorkflowState{
		ElementType:        el.Type,
		ElementId:          elementId,
		WorkflowInstanceId: wfiId,
		Vars:               vars,
	}); err != nil {
		return c.engineErr(ctx, span, "failed to publish workflow state", err, wfiIDAttr, wfIDAttr, elIDAttr, elNameAttr, elTypeAttr, wfNameAttr)
	}

	switch el.Type {
	case "serviceTask":
		if err := c.startJob(ctx, messages.WorkflowJobServiceTaskExecute, wfiId, el, vars); err != nil {
			return c.engineErr(ctx, span, "failed to start job", err, wfiIDAttr, wfIDAttr, elIDAttr, elNameAttr, elTypeAttr, wfNameAttr)
		}
	case "UserTask":
		if err := c.startJob(ctx, messages.WorkflowJobUserTaskExecute, wfiId, el, vars); err != nil {
			return c.engineErr(ctx, span, "failed to start job", err, wfiIDAttr, wfIDAttr, elIDAttr, elNameAttr, elTypeAttr, wfNameAttr)
		}
	case "ManualTask":
		if err := c.startJob(ctx, messages.WorkflowJobManualTaskExecute, wfiId, el, vars); err != nil {
			return c.engineErr(ctx, span, "failed to start job", err, wfiIDAttr, wfIDAttr, elIDAttr, elNameAttr, elTypeAttr, wfNameAttr)
		}
	case "callActivity":
		if _, err := c.launch(ctx, el.Execute, vars, wfiId, el.Id); err != nil {
			return c.engineErr(ctx, span, "failed to launch child workflow", err, wfiIDAttr, wfIDAttr, elIDAttr, elNameAttr, elTypeAttr, wfNameAttr)
		}
	case "endEvent":
		ctx, span := tracer.Start(ctx, "WorkflowInstanceComplete")
		defer span.End()
		parentWfiIDAttr := attribute.KeyValue{Key: keys.ParentWorkflowInstanceId, Value: attribute.StringValue(wfi.ParentWorkflowInstanceId)}
		span.SetAttributes(parentWfiIDAttr, wfIDAttr, elIDAttr, elNameAttr, elTypeAttr, wfiIDAttr, wfNameAttr)
		if wfi.ParentWorkflowInstanceId != "" {
			if err := c.returnBack(ctx, wfi.WorkflowInstanceId, wfi.ParentWorkflowInstanceId, wfi.ParentElementId, vars); err != nil {
				return c.engineErr(ctx, span, "failed to return to originator workflow", err, wfiIDAttr, wfIDAttr, elIDAttr, elNameAttr, elTypeAttr, parentWfiIDAttr, wfNameAttr)
			}
		}
		if err := c.cleanup(ctx, wfi.WorkflowInstanceId); err != nil {
			return c.engineErr(ctx, span, "failed to return to remove workflow instance", err, wfiIDAttr, wfIDAttr, elIDAttr, elNameAttr, elTypeAttr, wfNameAttr)
		}
	default:
		if err := c.traverse(ctx, wfi, el.Outbound, els, vars); err != nil {
			return c.engineErr(ctx, span, "failed to return to traverse", err, wfiIDAttr, wfIDAttr, elIDAttr, elNameAttr, elTypeAttr, wfNameAttr)
		}
	}
	return nil
}

func (c *Engine) returnBack(ctx context.Context, wfiID string, parentWfiID string, parentElID string, vars []byte) error {
	wfiIDattr := attribute.String(keys.WorkflowInstanceID, wfiID)
	parentWfiIDattr := attribute.String(keys.ParentWorkflowInstanceId, parentWfiID)
	elIDattr := attribute.String(keys.ElementID, parentElID)
	ctx, span := tracer.Start(ctx, "WorkflowReturn", trace.WithAttributes(wfiIDattr, elIDattr, parentWfiIDattr))
	defer span.End()
	pwfi, err := c.store.GetWorkflowInstance(ctx, parentWfiID)
	if err == errors.ErrWorkflowInstanceNotFound {
		span.SetStatus(codes.Error, "parent workflow instance not found, cancelling")
		c.log.Ctx(ctx).Warn("parent workflow instance not found, cancelling return to caller", zap.Error(err), zap.String(keys.ParentWorkflowInstanceId, parentWfiID))
		return nil
	} else if err != nil {
		return fmt.Errorf("failed to fetch workflow instance for return back: %w", err)
	}
	pwf, err := c.store.GetWorkflow(ctx, pwfi.WorkflowId)
	if err != nil {
		return c.engineErr(ctx, span, "failed to fetch return workflow", err, wfiIDattr, elIDattr, parentWfiIDattr)
	}
	index := make(map[string]*model.Element)
	indexElements(pwf.Elements, index)
	el := index[parentElID]
	err = c.traverse(ctx, pwfi, el.Outbound, index, vars)
	if err != nil {
		elNameAttr := attribute.KeyValue{Key: keys.ElementName, Value: attribute.StringValue(el.Name)}
		elTypeAttr := attribute.KeyValue{Key: keys.ElementType, Value: attribute.StringValue(el.Type)}
		return c.engineErr(ctx, span, "failed to traverse", err, wfiIDattr, elIDattr, elTypeAttr, elNameAttr, parentWfiIDattr)
	}
	return nil
}

func (c *Engine) cleanup(ctx context.Context, wfiId string) error {
	if err := c.store.DestroyWorkflowInstance(ctx, wfiId); err != nil {
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
	ctx, span := tracer.Start(ctx, "JobComplete")
	defer span.End()
	job, err := c.store.GetJob(ctx, jobId)
	if err != nil {
		return c.engineErr(ctx, span, "failed to locate job", err, attribute.KeyValue{Key: keys.JobID, Value: attribute.StringValue(jobId)})
	}

	jobIdAttr := attribute.KeyValue{Key: keys.JobID, Value: attribute.StringValue(job.Id)}
	jobTypeAttr := attribute.KeyValue{Key: keys.JobType, Value: attribute.StringValue(job.JobType)}
	span.SetAttributes(jobIdAttr, jobTypeAttr)

	wfi, err := c.store.GetWorkflowInstance(ctx, job.WfiID)
	if err == errors.ErrWorkflowInstanceNotFound {
		span.SetStatus(codes.Error, "workflow instance not found, cancelling")
		c.log.Ctx(ctx).Warn("workflow instance not found, cancelling job processing", zap.Error(err), zap.String(keys.WorkflowInstanceID, job.WfiID))
		return nil
	} else if err != nil {
		return c.engineErr(ctx, span, "failed to get workflow instance for job", err, jobTypeAttr, jobIdAttr)
	}
	wfiIDAttr := attribute.KeyValue{Key: keys.WorkflowInstanceID, Value: attribute.StringValue(wfi.WorkflowInstanceId)}
	wfIDAttr := attribute.KeyValue{Key: keys.WorkflowID, Value: attribute.StringValue(wfi.WorkflowId)}
	span.SetAttributes(wfiIDAttr, wfIDAttr)

	wf, err := c.store.GetWorkflow(ctx, wfi.WorkflowId)
	if err != nil {
		return c.engineErr(ctx, span, "failed to fetch job workflow", err, jobTypeAttr, wfiIDAttr, jobIdAttr, wfIDAttr)
	}
	els := make(map[string]*model.Element)
	indexElements(wf.Elements, els)
	el := els[job.ElementId]
	elIDAttr := attribute.KeyValue{Key: keys.ElementID, Value: attribute.StringValue(el.Id)}
	elNameAttr := attribute.KeyValue{Key: keys.ElementName, Value: attribute.StringValue(el.Name)}
	elTypeAttr := attribute.KeyValue{Key: keys.ElementType, Value: attribute.StringValue(el.Type)}
	span.SetAttributes(elIDAttr, elNameAttr, elTypeAttr)
	if err := c.traverse(ctx, wfi, el.Outbound, els, vars); err != nil {
		return c.engineErr(ctx, span, "failed to launch traversal", err, wfiIDAttr, wfIDAttr, elIDAttr, elNameAttr, elTypeAttr, jobIdAttr, jobTypeAttr)
	}
	return nil
}

func (c *Engine) startJob(ctx context.Context, subject string, wfiId string, el *model.Element, vars []byte) error {

	elIDAttr := attribute.KeyValue{Key: keys.ElementID, Value: attribute.StringValue(el.Id)}
	elNameAttr := attribute.KeyValue{Key: keys.ElementName, Value: attribute.StringValue(el.Name)}
	elTypeAttr := attribute.KeyValue{Key: keys.ElementType, Value: attribute.StringValue(el.Type)}
	jobTypeAttr := attribute.KeyValue{Key: keys.JobType, Value: attribute.StringValue(el.Type)}
	wfiIDAttr := attribute.KeyValue{Key: keys.WorkflowInstanceID, Value: attribute.StringValue(wfiId)}
	ctx, span := tracer.Start(ctx, "JobStart", trace.WithAttributes(elNameAttr, elIDAttr, elTypeAttr, jobTypeAttr, wfiIDAttr))
	defer span.End()
	job := &model.Job{WfiID: wfiId, Vars: vars, JobType: el.Type, ElementId: el.Id, Execute: el.Execute}
	jobId, err := c.store.CreateJob(ctx, job)

	jobIDAttr := attribute.KeyValue{Key: keys.JobID, Value: attribute.StringValue(jobId)}
	span.SetAttributes(jobIDAttr)

	if err != nil {
		return c.engineErr(ctx, span, "failed to start manual task", err, elNameAttr, elIDAttr, elTypeAttr, jobIDAttr, jobTypeAttr, wfiIDAttr)
	}
	job.Id = jobId
	return c.queue.PublishJob(ctx, subject, el, job)
}

// elementTable indexes an entire process for quick Id lookups
func elementTable(process *model.Process) map[string]*model.Element {
	el := make(map[string]*model.Element)
	indexElements(process.Elements, el)
	return el
}

// indexElements is the recursive part of the index
func indexElements(elements []*model.Element, el map[string]*model.Element) {
	for _, i := range elements {
		el[i.Id] = i
		if i.Process != nil {
			indexElements(i.Process.Elements, el)
		}
	}
}

func (c *Engine) engineErr(ctx context.Context, span trace.Span, msg string, err error, attrs ...attribute.KeyValue) error {
	z := keyvalueToZap(attrs...)
	z = append(z, zap.Error(err))
	c.log.Ctx(ctx).Error(msg, z...)
	span.RecordError(err, trace.WithAttributes(attrs...))
	span.SetStatus(codes.Error, msg)
	return err
}

func (c *Engine) Shutdown() {
	select {
	case <-c.closing:
		return
	default:
		close(c.closing)
		return
	}
}

func (c *Engine) CancelWorkflowInstance(ctx context.Context, id string) error {
	return c.store.DestroyWorkflowInstance(ctx, id)
}

func keyvalueToZap(attrs ...attribute.KeyValue) []zapcore.Field {
	ret := make([]zapcore.Field, len(attrs))
	for i, kv := range attrs {
		ret[i] = zap.String(string(kv.Key), kv.Value.AsString())
	}
	return ret
}
