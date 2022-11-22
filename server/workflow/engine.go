package workflow

import (
	"context"
	errors2 "errors"
	"fmt"
	"github.com/nats-io/nats.go"
	"github.com/segmentio/ksuid"
	"gitlab.com/shar-workflow/shar/client/parser"
	"gitlab.com/shar-workflow/shar/common"
	"gitlab.com/shar-workflow/shar/common/expression"
	"gitlab.com/shar-workflow/shar/common/logx"
	"gitlab.com/shar-workflow/shar/common/subj"
	"gitlab.com/shar-workflow/shar/model"
	"gitlab.com/shar-workflow/shar/server/errors"
	"gitlab.com/shar-workflow/shar/server/errors/keys"
	"gitlab.com/shar-workflow/shar/server/messages"
	"gitlab.com/shar-workflow/shar/server/services"
	"gitlab.com/shar-workflow/shar/server/vars"
	"golang.org/x/exp/slog"
	"strconv"
	"time"
)

// Engine contains the workflow processing functions
type Engine struct {
	closing chan struct{}
	ns      NatsService
}

// NewEngine returns an instance of the core workflow engine.
func NewEngine(ns NatsService) (*Engine, error) {
	e := &Engine{
		ns:      ns,
		closing: make(chan struct{}),
	}
	return e, nil
}

// Start sets up the activity and job processors and starts the engine processing workflows.
func (c *Engine) Start(ctx context.Context) error {
	// Start the workflow event processor.  This processes all the workflow states.
	c.ns.SetEventProcessor(c.activityStartProcessor)

	// Start the competed job processor.  This processes any tasks completed by the client.
	c.ns.SetCompleteJobProcessor(c.completeJobProcessor)

	// Start the message processor.  This processes received intra workflow messages.
	c.ns.SetMessageCompleteProcessor(c.messageCompleteProcessor)

	// Set traversal function
	c.ns.SetTraversalProvider(c.traverse)

	// Set the completed activity processor
	c.ns.SetCompleteActivityProcessor(c.activityCompleteProcessor)

	// Set the completed activity processor
	c.ns.SetCompleteActivity(c.completeActivity)

	c.ns.SetMessageProcessor(c.timedExecuteProcessor)

	c.ns.SetLaunchFunc(c.launchProcessor)

	c.ns.SetAbort(c.abortProcessor)

	return c.ns.StartProcessing(ctx)
}

// LoadWorkflow loads a model.Process describing a workflow into the engine ready for execution.
func (c *Engine) LoadWorkflow(ctx context.Context, model *model.Workflow) (string, error) {
	// Store the workflow model and return an ID
	wfID, err := c.ns.StoreWorkflow(ctx, model)
	if err != nil {
		return "", fmt.Errorf("failed to store workflow: %w", err)
	}
	return wfID, nil
}

// Launch starts a new instance of a workflow and returns a workflow instance Id.
func (c *Engine) Launch(ctx context.Context, workflowName string, vars []byte) (string, error) {
	return c.launch(ctx, workflowName, []string{}, vars, "", "")
}

// launch contains the underlying logic to start a workflow.  It is also called to spawn new instances of child workflows.
func (c *Engine) launch(ctx context.Context, workflowName string, ID common.TrackingID, vrs []byte, parentwfiID string, parentElID string) (string, error) {
	ctx, log := logx.ContextWith(ctx, "engine.launch")
	// get the last ID of the workflow
	wfID, err := c.ns.GetLatestVersion(ctx, workflowName)
	if err != nil {
		return "", c.engineErr(ctx, "failed to get latest version of workflow", err,
			slog.String(keys.ParentInstanceElementID, parentElID),
			slog.String(keys.ParentWorkflowInstanceID, parentwfiID),
			slog.String(keys.WorkflowName, workflowName),
		)
	}

	// get the last version of the workflow
	wf, err := c.ns.GetWorkflow(ctx, wfID)
	if err != nil {
		return "", c.engineErr(ctx, "failed to get workflow", err,
			slog.String(keys.ParentInstanceElementID, parentElID),
			slog.String(keys.ParentWorkflowInstanceID, parentwfiID),
			slog.String(keys.WorkflowName, workflowName),
			slog.String(keys.WorkflowID, wfID),
		)
	}
	testVars, err := vars.Decode(ctx, vrs)
	if err != nil {
		return "", fmt.Errorf("failed to decode variables during launch: %w", err)
	}
	var hasStartEvents bool
	// Test to see if all variables are present.
	vErr := forEachStartElement(wf, func(el *model.Element) error {
		hasStartEvents = true
		if el.OutputTransform != nil {
			for _, v := range el.OutputTransform {
				evs, err := expression.GetVariables(v)
				if err != nil {
					return fmt.Errorf("failed to extract variables from workflow during launch: %w", err)
				}
				for ev := range evs {
					if _, ok := testVars[ev]; !ok {
						return errors2.New("Workflow expects variable: " + ev)
					}
				}
			}
		}
		return nil
	})

	if vErr != nil {
		return "", fmt.Errorf("failed to initialize all workflow start events: %w", err)
	}

	// Start all timed start events.
	vErr = forEachTimedStartElement(wf, func(el *model.Element) error {
		if el.Type == "timedStartEvent" {
			timer := &model.WorkflowState{
				Id:           ID.Push(ksuid.New().String()),
				WorkflowId:   wfID,
				ElementId:    el.Id,
				UnixTimeNano: time.Now().UnixNano(),
				Timer: &model.WorkflowTimer{
					LastFired: 0,
					Count:     0,
				},
				Vars: vrs,
			}
			return c.ns.PublishWorkflowState(ctx, subj.NS(messages.WorkflowTimedExecute, "default"), timer)
		}
		return nil
	})

	if hasStartEvents {
		// create a workflow instance
		wfi, err := c.ns.CreateWorkflowInstance(ctx,
			&model.WorkflowInstance{
				WorkflowId:               wfID,
				ParentWorkflowInstanceId: &parentwfiID,
				ParentElementId:          &parentElID,
			})
		if err != nil {
			return "", c.engineErr(ctx, "failed to create workflow instance", err,
				slog.String(keys.ParentInstanceElementID, parentElID),
				slog.String(keys.ParentWorkflowInstanceID, parentwfiID),
				slog.String(keys.WorkflowName, workflowName),
				slog.String(keys.WorkflowID, wfID),
			)
		}

		if vErr != nil {
			return "", vErr
		}

		errs := make(chan error)

		state := &model.WorkflowState{
			Id:                 ID.Pop().Push(wfi.WorkflowInstanceId),
			WorkflowInstanceId: wfi.WorkflowInstanceId,
			WorkflowId:         wfID,
			Vars:               vrs,
		}

		// fire off the new workflow state
		if err := c.ns.PublishWorkflowState(ctx, messages.WorkflowInstanceExecute, state); err != nil {
			errs <- c.engineErr(ctx, "failed to publish workflow state", err,
				slog.String(keys.WorkflowName, workflowName),
				slog.String(keys.WorkflowID, wfID),
			)
		}

		// for each start element, launch a workflow thread
		startErr := forEachStartElement(wf, func(el *model.Element) error {
			trackingID := ID.Push(wfi.WorkflowInstanceId).Push(ksuid.New().String())
			if err := c.ns.PublishWorkflowState(ctx, messages.WorkflowTraversalExecute, &model.WorkflowState{
				ElementType:        el.Type,
				ElementId:          el.Id,
				WorkflowId:         wfi.WorkflowId,
				WorkflowInstanceId: wfi.WorkflowInstanceId,
				Id:                 trackingID,
				Vars:               vrs,
			}); err != nil {
				log.Error("failed to publish initial traversal", err)
				return fmt.Errorf("failed to publish initial traversal: %w", err)
			}
			return nil
		})
		if startErr != nil {
			return "", startErr
		}
		// wait for all paths to be started
		close(errs)
		if err := <-errs; err != nil {
			return "", c.engineErr(ctx, "failed initial traversal", err,
				slog.String(keys.ParentInstanceElementID, parentElID),
				slog.String(keys.ParentWorkflowInstanceID, parentwfiID),
				slog.String(keys.WorkflowName, workflowName),
				slog.String(keys.WorkflowID, wfID),
			)
		}
		return wfi.WorkflowInstanceId, nil
	}
	return "", nil
}

// forEachStartElement finds all start elements for a given process and executes a function on the element.
func forEachStartElement(wf *model.Workflow, fn func(element *model.Element) error) error {
	for _, pr := range wf.Process {
		for _, i := range pr.Elements {
			if i.Type == "startEvent" {
				err := fn(i)
				if err != nil {
					return err
				}
			}
		}
	}
	// TODO: Ensure workflow terminates
	return nil
}

// forEachStartElement finds all start elements for a given process and executes a function on the element.
func forEachTimedStartElement(wf *model.Workflow, fn func(element *model.Element) error) error {
	for _, pr := range wf.Process {
		for _, i := range pr.Elements {
			if i.Type == "timedStartEvent" {
				err := fn(i)
				if err != nil {
					return err
				}
			}
		}
	}
	// TODO: Ensure workflow terminates
	return nil
}

// traverse traverses all outbound connections provided the conditions passed if available.
func (c *Engine) traverse(ctx context.Context, wfi *model.WorkflowInstance, trackingID common.TrackingID, outbound *model.Targets, el map[string]*model.Element, v []byte) error {
	ctx, log := logx.ContextWith(ctx, "engine.traverse")
	if outbound == nil {
		return nil
	}
	// Traverse along all outbound edges
	for _, t := range outbound.Target {
		ok := true
		// Evaluate conditions
		for _, ex := range t.Conditions {

			// TODO: Cache compilation.
			exVars, err := vars.Decode(ctx, v)
			if err != nil {
				return err
			}

			// evaluate the condition
			res, err := expression.Eval[bool](ctx, ex, exVars)
			if err != nil {
				return &errors.ErrWorkflowFatal{Err: err}
			}
			if !res {
				ok = false
				break
			}
		}

		target := el[t.Target]

		// If the conditions passed commit a traversal
		if ok {
			tID := trackingID.Push(ksuid.New().String())
			if err := c.ns.PublishWorkflowState(ctx, messages.WorkflowTraversalExecute, &model.WorkflowState{
				ElementType:        target.Type,
				ElementId:          target.Id,
				WorkflowId:         wfi.WorkflowId,
				WorkflowInstanceId: wfi.WorkflowInstanceId,
				Id:                 tID,
				Vars:               v,
			}); err != nil {
				log.Error("failed to publish workflow state", err)
				return err
			}

			if outbound.Exclusive {
				break
			}
		}
	}
	return nil
}

// activityStartProcessor handles the behaviour of each BPMN element
func (c *Engine) activityStartProcessor(ctx context.Context, newActivityID string, traversal *model.WorkflowState, traverseOnly bool) error {
	ctx, log := logx.ContextWith(ctx, "engine.activityStartProcessor")
	// set the default status to be 'executing'
	status := model.CancellationState_executing

	// get the corresponding workflow instance
	wfi, err := c.ns.GetWorkflowInstance(ctx, traversal.WorkflowInstanceId)
	if err == errors.ErrWorkflowInstanceNotFound || errors2.Is(err, nats.ErrKeyNotFound) {
		// if the workflow instance has been removed kill any activity and exit
		log.Warn("workflow instance not found, cancelling activity", err, slog.String(keys.WorkflowInstanceID, traversal.WorkflowInstanceId))
		return nil
	} else if err != nil {
		return c.engineErr(ctx, "failed to get workflow instance", err,
			slog.String(keys.WorkflowInstanceID, traversal.WorkflowInstanceId),
		)
	}

	// get the corresponding workflow definition
	process, err := c.ns.GetWorkflow(ctx, wfi.WorkflowId)
	if err != nil {
		return c.engineErr(ctx, "failed to get workflow", err,
			slog.String(keys.WorkflowInstanceID, wfi.WorkflowInstanceId),
			slog.String(keys.WorkflowID, wfi.WorkflowId),
		)
	}

	// create an indexed map of elements
	els := common.ElementTable(process)
	el := els[traversal.ElementId]

	// force traversal will not process the event, and will just traverse instead.
	if traverseOnly {
		el.Type = "forceTraversal"
	}

	activityID := common.TrackingID(traversal.Id).Pop().Push(newActivityID)

	// tell the world we are going to execute an activity
	if err := c.ns.PublishWorkflowState(ctx, messages.WorkflowActivityExecute, &model.WorkflowState{
		Id:                 activityID,
		ElementType:        el.Type,
		ElementId:          traversal.ElementId,
		WorkflowInstanceId: traversal.WorkflowInstanceId,
		WorkflowId:         wfi.WorkflowId,
		Vars:               traversal.Vars,
	}); err != nil {
		return c.engineErr(ctx, "failed to publish workflow status", err, apErrFields(wfi.WorkflowInstanceId, wfi.WorkflowId, el.Id, el.Name, el.Type, process.Name)...)
	}
	// tell the world we have safely completed the traversal
	if err := c.ns.PublishWorkflowState(ctx, messages.WorkflowTraversalComplete, &model.WorkflowState{
		ElementType:        el.Type,
		ElementId:          traversal.ElementId,
		WorkflowId:         wfi.WorkflowId,
		WorkflowInstanceId: traversal.WorkflowInstanceId,
		Id:                 traversal.Id,
		Vars:               traversal.Vars,
	}); err != nil {
		return c.engineErr(ctx, "failed to publish workflow status", err, apErrFields(wfi.WorkflowInstanceId, wfi.WorkflowId, el.Id, el.Name, el.Type, process.Name)...)
	}

	//Start any timers
	if el.BoundaryTimer != nil || len(el.BoundaryTimer) > 0 {
		for _, i := range el.BoundaryTimer {
			timer := &model.WorkflowState{
				Id:                 activityID,
				WorkflowId:         traversal.WorkflowId,
				WorkflowInstanceId: traversal.WorkflowInstanceId,
				ElementId:          traversal.ElementId,
				Execute:            &i.Target,
				UnixTimeNano:       time.Now().UnixNano(),
				Timer: &model.WorkflowTimer{
					LastFired: 0,
					Count:     0,
				},
			}
			v, err := vars.Decode(ctx, traversal.Vars)
			if err != nil {
				return err
			}
			res, err := expression.EvalAny(ctx, i.Duration, v)
			if err != nil {
				return err
			}
			ut := time.Now()
			switch x := res.(type) {
			case int:
				ut = ut.Add(time.Duration(x))
			case string:
				dur, err := parser.ParseISO8601(x)
				if err != nil {
					return err
				}
				ut = dur.Shift(ut)
			}
			err = c.ns.PublishWorkflowState(ctx, subj.NS(messages.WorkflowElementTimedExecute, "default"), timer, services.WithEmbargo(int(ut.UnixNano())))
			if err != nil {
				return err
			}
		}
	}

	// process any supported events
	switch el.Type {
	case "startEvent":
		initVars := make([]byte, 0)
		err := vars.OutputVars(ctx, traversal.Vars, &initVars, el.OutputTransform)
		if err != nil {
			return err
		}
		traversal.Vars = initVars
		if err := c.completeActivity(ctx, activityID, el, wfi, status, traversal.Vars); err != nil {
			return err
		}
	case "exclusiveGateway":
		if err := c.completeActivity(ctx, activityID, el, wfi, status, traversal.Vars); err != nil {
			return err
		}
	case "serviceTask":
		stID, err := c.ns.GetServiceTaskRoutingKey(el.Execute)
		if err != nil {
			return err
		}
		if err := c.startJob(ctx, messages.WorkflowJobServiceTaskExecute+"."+stID, wfi.WorkflowId, traversal.WorkflowInstanceId, activityID, el, "", traversal.Vars); err != nil {
			return c.engineErr(ctx, "failed to start srvice task job", err, apErrFields(wfi.WorkflowInstanceId, wfi.WorkflowId, el.Id, el.Name, el.Type, process.Name)...)
		}
	case "userTask":
		if err := c.startJob(ctx, messages.WorkflowJobUserTaskExecute, wfi.WorkflowId, traversal.WorkflowInstanceId, activityID, el, "", traversal.Vars); err != nil {
			return c.engineErr(ctx, "failed to start user task job", err, apErrFields(wfi.WorkflowInstanceId, wfi.WorkflowId, el.Id, el.Name, el.Type, process.Name)...)
		}
	case "manualTask":
		if err := c.startJob(ctx, messages.WorkflowJobManualTaskExecute, wfi.WorkflowId, traversal.WorkflowInstanceId, activityID, el, "", traversal.Vars); err != nil {
			return c.engineErr(ctx, "failed to start manual task job", err, apErrFields(wfi.WorkflowInstanceId, wfi.WorkflowId, el.Id, el.Name, el.Type, process.Name)...)
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
		sendMsgID, err := c.ns.GetMessageSenderRoutingKey(wf.Name, wf.Messages[ix].Name)
		if err != nil {
			return err
		}
		if err := c.startJob(ctx, messages.WorkflowJobSendMessageExecute+"."+sendMsgID, wfi.WorkflowId, traversal.WorkflowInstanceId, activityID, el, wf.Messages[ix].Execute, traversal.Vars); err != nil {
			return c.engineErr(ctx, "failed to start message job", err, apErrFields(wfi.WorkflowInstanceId, wfi.WorkflowId, el.Id, el.Name, el.Type, process.Name)...)
		}
	case "callActivity":
		if err := c.startJob(ctx, subj.NS(messages.WorkflowJobLaunchExecute, "default"), wfi.WorkflowId, wfi.WorkflowInstanceId, activityID, el, "", traversal.Vars); err != nil {
			return c.engineErr(ctx, "failed to start message lauch", err, apErrFields(wfi.WorkflowInstanceId, wfi.WorkflowId, el.Id, el.Name, el.Type, process.Name)...)
		}
	case "messageIntermediateCatchEvent":
		if err := c.awaitMessage(ctx, wfi.WorkflowId, traversal.WorkflowInstanceId, activityID, el, traversal.Vars); err != nil {
			return c.engineErr(ctx, "failed to await message", err, apErrFields(wfi.WorkflowInstanceId, wfi.WorkflowId, el.Id, el.Name, el.Type, process.Name)...)
		}
	case "timerIntermediateCatchEvent":
		varmap, err := vars.Decode(ctx, traversal.Vars)
		if err != nil {
			return &errors.ErrWorkflowFatal{Err: err}
		}
		ret, err := expression.EvalAny(ctx, el.Execute, varmap)
		if err != nil {
			return &errors.ErrWorkflowFatal{Err: err}
		}
		var embargo int
		switch em := ret.(type) {
		case string:
			if v, err := strconv.Atoi(em); err == nil {
				embargo = v + int(time.Now().UnixNano())
				break
			}
			pem, err := parser.ParseISO8601(em)
			if err != nil {
				return &errors.ErrWorkflowFatal{Err: err}
			}
			embargo = int(pem.Shift(time.Now()).UnixNano())
		case int:
			embargo = em + int(time.Now().UnixNano())
		default:
			return errors.ErrFatalBadDuration
		}
		traversal.Id = activityID.Push(ksuid.New().String())
		if err := c.ns.PublishWorkflowState(ctx, subj.NS(messages.WorkflowJobTimerTaskExecute, "default"), traversal); err != nil {
			return err
		}
		if err := c.ns.PublishWorkflowState(ctx, subj.NS(messages.WorkflowJobTimerTaskComplete, "default"), traversal, services.WithEmbargo(embargo)); err != nil {
			return err
		}
	case "endEvent":
		if wfi.ParentWorkflowInstanceId == nil || *wfi.ParentWorkflowInstanceId == "" {
			if len(el.Errors) == 0 {
				status = model.CancellationState_completed
			} else {
				status = model.CancellationState_errored
			}
		}
		if err := c.completeActivity(ctx, activityID, el, wfi, status, traversal.Vars); err != nil {
			return err
		}
	default:
		// if we don't support the event, just traverse to the next element
		if err := c.traverse(ctx, wfi, activityID, el.Outbound, els, traversal.Vars); errors.IsWorkflowFatal(err) {
			log.Error("workflow fatally terminated whilst traversing", err, slog.String(keys.WorkflowInstanceID, wfi.WorkflowInstanceId), slog.String(keys.WorkflowID, wfi.WorkflowId), err, slog.String(keys.ElementName, el.Name))
			return nil
		} else if err != nil {
			return c.engineErr(ctx, "failed to return to traverse", err, apErrFields(wfi.WorkflowInstanceId, wfi.WorkflowId, el.Id, el.Name, el.Type, process.Name)...)
		}
		if err := c.completeActivity(ctx, activityID, el, wfi, status, traversal.Vars); err != nil {
			return err
		}
	}

	// if the workflow is complete, send an instance complete message to trigger tidy up
	if status == model.CancellationState_completed || status == model.CancellationState_errored || status == model.CancellationState_terminated {
		if err := c.ns.PublishWorkflowState(ctx, messages.WorkflowInstanceComplete, &model.WorkflowState{
			Id:                 []string{wfi.WorkflowInstanceId},
			WorkflowId:         wfi.WorkflowId,
			WorkflowInstanceId: traversal.WorkflowInstanceId,
			ElementId:          traversal.ElementId,
			ElementType:        el.Type,
			Error:              el.Error,
			State:              status,
		}); err != nil {
			return c.engineErr(ctx, "failed to publish workflow status", err, apErrFields(wfi.WorkflowInstanceId, wfi.WorkflowId, el.Id, el.Name, el.Type, process.Name)...)
		}
	}
	return nil
}

func (c *Engine) completeActivity(ctx context.Context, trackingID common.TrackingID, el *model.Element, wfi *model.WorkflowInstance, cancellationState model.CancellationState, vrs []byte) error {
	// tell the world that we processed the activity
	if err := c.ns.PublishWorkflowState(ctx, messages.WorkflowActivityComplete, &model.WorkflowState{
		Id:                 trackingID,
		ElementType:        el.Type,
		ElementId:          el.Id,
		WorkflowId:         wfi.WorkflowId,
		WorkflowInstanceId: wfi.WorkflowInstanceId,
		State:              cancellationState,
		Error:              el.Error,
		Vars:               vrs,
	}); err != nil {
		return c.engineErr(ctx, "failed to publish workflow cancellationState", err)
		//TODO: report this without process: apErrFields(wfi.WorkflowInstanceId, wfi.WorkflowId, el.Id, el.Name, el.Type, process.Name)
	}
	return nil
}

// apErrFields writes out the common error fields for an application error
func apErrFields(workflowInstanceID, workflowID, elementID, elementName, elementType, workflowName string, extraFields ...any) []any {
	fields := []any{
		slog.String(keys.WorkflowInstanceID, workflowInstanceID),
		slog.String(keys.WorkflowID, workflowID),
		slog.String(keys.ElementID, elementID),
		slog.String(keys.ElementName, elementName),
		slog.String(keys.ElementType, elementType),
		slog.String(keys.WorkflowName, workflowName),
	}
	if len(extraFields) > 0 {
		fields = append(fields, extraFields...)
	}
	return fields
}

// completeJobProcessor processes completed jobs
func (c *Engine) completeJobProcessor(ctx context.Context, job *model.WorkflowState) error {
	ctx, log := logx.ContextWith(ctx, "engine.completeJobProcessor")
	// Validate if it safe to end this job
	// Get the saved job state
	if _, err := c.ns.GetOldState(common.TrackingID(job.Id).ParentID()); err == errors.ErrStateNotFound {
		// We can't find the job's saved state
		return nil
	} else if err != nil {
		return err
	}

	// get the relevant workflow instance
	wfi, err := c.ns.GetWorkflowInstance(ctx, job.WorkflowInstanceId)
	if err == errors.ErrWorkflowInstanceNotFound {
		// if the instance has been deleted quash this activity
		log.Warn("workflow instance not found, cancelling job processing", err, slog.String(keys.WorkflowInstanceID, job.WorkflowInstanceId))
		return nil
	} else if err != nil {
		log.Warn("failed to get workflow instance for job", err,
			slog.String(keys.JobType, job.ElementType),
			slog.String(keys.JobID, common.TrackingID(job.Id).ID()),
		)
		return err
	}

	// get the relevant workflow
	wf, err := c.ns.GetWorkflow(ctx, wfi.WorkflowId)
	if err != nil {
		return c.engineErr(ctx, "failed to fetch job workflow", err,
			slog.String(keys.JobType, job.ElementType),
			slog.String(keys.JobID, common.TrackingID(job.Id).ID()),
			slog.String(keys.WorkflowInstanceID, wfi.WorkflowInstanceId),
			slog.String(keys.WorkflowID, wfi.WorkflowId),
		)
	}
	// build element table
	els := common.ElementTable(wf)
	el := els[job.ElementId]
	newID := common.TrackingID(job.Id).Pop()
	oldState, err := c.ns.GetOldState(newID.ID())
	if err == errors.ErrStateNotFound {
		return nil
	}
	if err != nil {
		return err
	}
	if err := vars.OutputVars(ctx, job.Vars, &oldState.Vars, el.OutputTransform); err != nil {
		return err
	}
	if err := c.completeActivity(ctx, newID, el, wfi, job.State, oldState.Vars); err != nil {
		return err
	}
	if err := c.ns.DeleteJob(ctx, common.TrackingID(job.Id).ID()); err != nil {
		return err
	}
	return nil
}

// startJob launches a user/service task
func (c *Engine) startJob(ctx context.Context, subject, wfID, wfiID string, trackingID common.TrackingID, el *model.Element, condition string, v []byte, opts ...services.PublishOpt) error {
	job := &model.WorkflowState{
		Id:                 trackingID,
		WorkflowId:         wfID,
		WorkflowInstanceId: wfiID,
		ElementType:        el.Type,
		ElementId:          el.Id,
		Error:              el.Error,
		Execute:            &el.Execute,
		Condition:          &condition,
	}
	err := vars.InputVars(ctx, v, &job.Vars, el)
	if err != nil {
		return err
	}
	vx, err := vars.Decode(ctx, v)
	if err != nil {
		return err
	}

	// if this is a user task, find out who can perfoem it
	if el.Type == "userTask" {
		owners, err := c.evaluateOwners(ctx, el.Candidates, vx)
		if err != nil {
			return err
		}
		groups, err := c.evaluateOwners(ctx, el.CandidateGroups, vx)
		if err != nil {
			return err
		}

		job.Owners = owners
		job.Groups = groups
	}

	// create the job
	_, err = c.ns.CreateJob(ctx, job)

	if err != nil {
		return c.engineErr(ctx, "failed to start manual task", err,
			slog.String(keys.ElementName, el.Name),
			slog.String(keys.ElementID, el.Id),
			slog.String(keys.ElementType, el.Type),
			slog.String(keys.JobType, job.ElementType),
			slog.String(keys.JobID, common.TrackingID(job.Id).ID()),
			slog.String(keys.WorkflowInstanceID, wfiID),
		)
	}
	// finally tell the engine that the job is ready for a client
	return c.ns.PublishWorkflowState(ctx, subj.NS(subject, "default"), job, opts...)
}

// evaluateOwners builds a list of groups
func (c *Engine) evaluateOwners(ctx context.Context, owners string, vars model.Vars) ([]string, error) {
	jobGroups := make([]string, 0)
	groups, err := expression.Eval[interface{}](ctx, owners, vars)
	if err != nil {
		return nil, &errors.ErrWorkflowFatal{Err: err}
	}
	switch groups := groups.(type) {
	case string:
		jobGroups = append(jobGroups, groups)
	case []string:
		jobGroups = append(jobGroups, groups...)
	}
	for i, v := range jobGroups {
		id, err := c.ns.OwnerID(v)
		if err != nil {
			return nil, err
		}
		jobGroups[i] = id
	}
	return jobGroups, nil
}

func (c *Engine) engineErr(ctx context.Context, msg string, err error, z ...any) error {
	log := slog.FromContext(ctx)
	log.Error(msg, err, z...)

	return err
}

// Shutdown gracefully stops the engine.
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

// CancelWorkflowInstance cancels a workflow instance with a reason.
func (c *Engine) CancelWorkflowInstance(ctx context.Context, id string, state model.CancellationState, wfError *model.Error) error {
	if state == model.CancellationState_executing {
		return errors2.New("executing is an invalid cancellation state")
	}
	return c.ns.DestroyWorkflowInstance(ctx, id, state, wfError)
}

// awaitMessage signals that the workflow instance will resume after a message is received
func (c *Engine) awaitMessage(ctx context.Context, wfID string, wfiID string, parentTrackingID common.TrackingID, el *model.Element, vars []byte) error {
	trackingID := parentTrackingID.Push(ksuid.New().String())
	awaitMsg := &model.WorkflowState{
		WorkflowId:         wfID,
		WorkflowInstanceId: wfiID,
		ElementId:          el.Id,
		ElementType:        el.Type,
		Error:              el.Error,
		Id:                 trackingID,
		Execute:            &el.Execute,
		Condition:          &el.Msg,
		Vars:               vars,
	}

	err := c.ns.AwaitMsg(ctx, awaitMsg)
	if err != nil {
		return c.engineErr(ctx, "failed to await message", err,
			slog.String(keys.ElementName, el.Name),
			slog.String(keys.ElementID, el.Id),
			slog.String(keys.ElementType, el.Type),
			slog.String(keys.JobType, awaitMsg.ElementType),
			slog.String(keys.WorkflowInstanceID, wfiID),
			slog.String(keys.Execute, *awaitMsg.Execute),
		)
	}
	return nil
}

func (c *Engine) messageCompleteProcessor(ctx context.Context, state *model.WorkflowState) error {
	ctx, log := logx.ContextWith(ctx, "engine.messageCompleteProcessor")
	wfi, err := c.ns.GetWorkflowInstance(ctx, state.WorkflowInstanceId)
	if err == nats.ErrKeyNotFound {
		log.Warn("workflow instance not found, cancelling message processing", err, slog.String(keys.WorkflowInstanceID, state.WorkflowInstanceId))
		return nil
	} else if err != nil {
		return err
	}
	wf, err := c.ns.GetWorkflow(ctx, state.WorkflowId)
	if err != nil {
		return err
	}
	els := common.ElementTable(wf)
	el := els[state.ElementId]
	id := common.TrackingID(state.Id).Pop()
	if err := c.completeActivity(ctx, id, el, wfi, state.State, state.Vars); err != nil {
		return err
	}
	return nil
}

// CompleteManualTask completes a manual workflow task
func (c *Engine) CompleteManualTask(ctx context.Context, trackingID string, newvars []byte) error {
	job, err := c.ns.GetJob(ctx, trackingID)
	if err != nil {
		return err
	}
	el, err := c.ns.GetElement(ctx, job)
	if err != nil {
		return &errors.ErrWorkflowFatal{Err: err}
	}
	job.Vars = newvars
	err = vars.CheckVars(ctx, job, el)
	if err != nil {
		return &errors.ErrWorkflowFatal{Err: err}
	}
	if err := c.ns.PublishWorkflowState(ctx, messages.WorkflowJobManualTaskComplete, job); err != nil {
		return err
	}
	return nil
}

// CompleteServiceTask completes a workflow service task
func (c *Engine) CompleteServiceTask(ctx context.Context, trackingID string, newvars []byte) error {
	job, err := c.ns.GetJob(ctx, trackingID)
	if err != nil {
		return err
	}
	if _, err := c.ns.GetOldState(common.TrackingID(job.Id).ParentID()); err == errors.ErrStateNotFound {
		if err := c.ns.PublishWorkflowState(ctx, subj.NS(messages.WorkflowJobServiceTaskAbort, "default"), job); err != nil {
			return err
		}
		return nil
	} else if err != nil {
		return err
	}
	el, err := c.ns.GetElement(ctx, job)
	if err != nil {
		return &errors.ErrWorkflowFatal{Err: err}
	}
	job.Vars = newvars
	err = vars.CheckVars(ctx, job, el)
	if err != nil {
		return &errors.ErrWorkflowFatal{Err: err}
	}
	if err := c.ns.PublishWorkflowState(ctx, messages.WorkflowJobServiceTaskComplete, job); err != nil {
		return err
	}
	return nil
}

// CompleteSendMessageTask completes a send message task
func (c *Engine) CompleteSendMessageTask(ctx context.Context, trackingID string, newvars []byte) error {
	job, err := c.ns.GetJob(ctx, trackingID)
	if err != nil {
		return err
	}
	_, err = c.ns.GetElement(ctx, job)
	if err != nil {
		return &errors.ErrWorkflowFatal{Err: err}
	}
	if err != nil {
		return &errors.ErrWorkflowFatal{Err: err}
	}
	if err := c.ns.PublishWorkflowState(ctx, messages.WorkflowJobSendMessageComplete, job); err != nil {
		return err
	}
	return nil
}

// CompleteUserTask completes and closes a user task with variables
func (c *Engine) CompleteUserTask(ctx context.Context, trackingID string, newvars []byte) error {
	job, err := c.ns.GetJob(ctx, trackingID)
	if err != nil {
		return err
	}
	el, err := c.ns.GetElement(ctx, job)
	if err != nil {
		return &errors.ErrWorkflowFatal{Err: err}
	}
	job.Vars = newvars
	err = vars.CheckVars(ctx, job, el)
	if err != nil {
		return &errors.ErrWorkflowFatal{Err: err}
	}
	if err := c.ns.PublishWorkflowState(ctx, messages.WorkflowJobUserTaskComplete, job); err != nil {
		return err
	}
	if err := c.ns.CloseUserTask(ctx, trackingID); err != nil {
		return err
	}
	return nil
}

func (c *Engine) activityCompleteProcessor(ctx context.Context, state *model.WorkflowState) error {
	ctx, log := logx.ContextWith(ctx, "engine.activityCompleteProcessor")
	if old, err := c.ns.GetOldState(common.TrackingID(state.Id).ID()); err == errors.ErrStateNotFound {
		return nil
	} else if err != nil {
		return err
	} else if old.State == model.CancellationState_obsolete && state.State == model.CancellationState_obsolete {
		return nil
	}
	wfi, err := c.ns.GetWorkflowInstance(ctx, state.WorkflowInstanceId)
	if err == errors.ErrWorkflowInstanceNotFound {
		errTxt := "workflow instance not found, cancelling message processing"
		log.Warn(errTxt, slog.String(keys.WorkflowInstanceID, state.WorkflowInstanceId))
		return nil
	} else if err != nil {
		return err
	}
	wf, err := c.ns.GetWorkflow(ctx, state.WorkflowId)
	if err != nil {
		return err
	}
	els := common.ElementTable(wf)
	newID := common.TrackingID(state.Id).Pop()
	if err = c.traverse(ctx, wfi, newID, els[state.ElementId].Outbound, els, state.Vars); errors.IsWorkflowFatal(err) {
		log.Error("workflow fatally terminated whilst traversing", err, slog.String(keys.WorkflowInstanceID, wfi.WorkflowInstanceId), slog.String(keys.WorkflowID, wfi.WorkflowId), slog.String(keys.ElementID, state.ElementId))
		return nil
	} else if err != nil {
		return err
	}
	if state.ElementType == "endEvent" && len(state.Id) > 2 {
		jobID := common.TrackingID(state.Id).Ancestor(2)
		// If we are a sub workflow then complete the parent job
		if jobID != state.WorkflowInstanceId {
			j, err := c.ns.GetJob(ctx, jobID)
			if err == errors.ErrJobNotFound {
				// This job was deleted, so just exit
				return nil
			} else if err != nil {
				return err
			}
			j.Vars = state.Vars
			j.Error = state.Error
			if err := c.ns.PublishWorkflowState(ctx, messages.WorkflowJobLaunchComplete, j); err != nil {
				return err
			}
			if err := c.ns.DeleteJob(ctx, jobID); err != nil {
				return err
			}
			if err := c.ns.DestroyWorkflowInstance(ctx, state.WorkflowInstanceId, state.State, state.Error); err != nil {
				return err
			}
		}
	}
	return nil
}

func (c *Engine) launchProcessor(ctx context.Context, state *model.WorkflowState) error {
	wf, err := c.ns.GetWorkflow(ctx, state.WorkflowId)
	if err != nil {
		return errors.ErrWorkflowFatal{Err: errors.ErrWorkflowNotFound}
	}
	els := common.ElementTable(wf)
	if _, err := c.launch(ctx, els[state.ElementId].Execute, state.Id, state.Vars, state.WorkflowInstanceId, state.ElementId); err != nil {
		return c.engineErr(ctx, "failed to launch child workflow", &errors.ErrWorkflowFatal{Err: err})
	}
	return nil
}

func (c *Engine) timedExecuteProcessor(ctx context.Context, state *model.WorkflowState) (bool, int, error) {
	ctx, log := logx.ContextWith(ctx, "engine.timedExecuteProcessor")
	wf, err := c.ns.GetWorkflow(ctx, state.WorkflowId)
	if err != nil {
		log.Error("could not get timer proto workflow: %s", err)
		return true, 0, err
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
		Vars: state.Vars,
	}

	var (
		isTimer    bool
		shouldFire bool
	)

	switch el.Timer.Type {
	case model.WorkflowTimerType_fixed:
		isTimer = true
		shouldFire = value <= now
	case model.WorkflowTimerType_duration:
		if repeat != 0 && count >= repeat {
			return true, 0, nil
		}
		isTimer = true
		shouldFire = elapsed >= value
	}

	if isTimer {
		if shouldFire {
			wfi, err := c.ns.CreateWorkflowInstance(ctx, &model.WorkflowInstance{
				WorkflowId: state.WorkflowId,
			})
			if err != nil {
				log.Error("error creating timed workflow instance", err)
				return false, 0, err
			}
			if err := vars.OutputVars(ctx, newTimer.Vars, &newTimer.Vars, el.OutputTransform); err != nil {
				log.Error("error merging variables", err)
				return false, 0, nil
			}
			if err := c.traverse(ctx, wfi, []string{ksuid.New().String()}, el.Outbound, els, newTimer.Vars); err != nil {
				log.Error("error traversing for timed workflow instance", err)
				return false, 0, nil
			}
			if err := c.ns.PublishWorkflowState(ctx, messages.WorkflowTimedExecute, newTimer); err != nil {
				log.Error("error publishing timer", err)
				return false, 0, nil
			}
		} else if el.Timer.Type == model.WorkflowTimerType_duration {
			return false, int(value - now), nil
		}
	}
	return true, 0, nil
}

func (c *Engine) abortProcessor(_ context.Context, abort services.AbortType, _ *model.WorkflowState) (bool, error) {
	switch abort {
	case services.AbortTypeActivity:
	case services.AbortTypeServiceTask:
	}
	return true, nil
}
