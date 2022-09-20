package workflow

import (
	"context"
	errors2 "errors"
	"fmt"
	"github.com/antonmedv/expr"
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
	"strconv"
	"sync"
	"time"
)

// Engine contains the workflow processing functions
type Engine struct {
	log     *zap.Logger
	closing chan struct{}
	ns      NatsService
}

// NewEngine returns an instance of the core workflow engine.
func NewEngine(log *zap.Logger, ns NatsService) (*Engine, error) {
	e := &Engine{
		ns:      ns,
		closing: make(chan struct{}),
		log:     log,
	}
	return e, nil
}

// Start sets up the activity and job processors and starts the engine processing workflows.
func (c *Engine) Start(ctx context.Context) error {
	// Start the workflow event processor.  This processes all the workflow states.
	c.ns.SetEventProcessor(c.activityProcessor)

	// Start the competed job processor.  This processes any tasks completed by the client.
	c.ns.SetCompleteJobProcessor(c.completeJobProcessor)

	// Start the message processor.  This processes received intra workflow messages.
	c.ns.SetMessageCompleteProcessor(c.messageCompleteProcessor)

	// Set traversal function
	c.ns.SetTraversalProvider(c.traverse)

	return c.ns.StartProcessing(ctx)
}

// LoadWorkflow loads a model.Process describing a workflow into the engine ready for execution.
func (c *Engine) LoadWorkflow(ctx context.Context, model *model.Workflow) (string, error) {
	// Store the workflow model and return an ID
	wfID, err := c.ns.StoreWorkflow(ctx, model)
	if err != nil {
		return "", err
	}
	return wfID, nil
}

// Launch starts a new instance of a workflow and returns a workflow instance Id.
func (c *Engine) Launch(ctx context.Context, workflowName string, vars []byte) (string, error) {
	return c.launch(ctx, workflowName, vars, "", "")
}

// launch contains the underlying logic to start a workflow.  It is also called to spawn new instances of child workflows.
func (c *Engine) launch(ctx context.Context, workflowName string, vrs []byte, parentwfiID string, parentElID string) (string, error) {
	// get the last ID of the workflow
	wfID, err := c.ns.GetLatestVersion(ctx, workflowName)
	if err != nil {
		return "", c.engineErr(ctx, "failed to get latest version of workflow", err,
			zap.String(keys.ParentInstanceElementID, parentElID),
			zap.String(keys.ParentWorkflowInstanceID, parentwfiID),
			zap.String(keys.WorkflowName, workflowName),
		)
	}

	// get the last version of the workflow
	wf, err := c.ns.GetWorkflow(ctx, wfID)
	if err != nil {
		return "", c.engineErr(ctx, "failed to get workflow", err,
			zap.String(keys.ParentInstanceElementID, parentElID),
			zap.String(keys.ParentWorkflowInstanceID, parentwfiID),
			zap.String(keys.WorkflowName, workflowName),
			zap.String(keys.WorkflowID, wfID),
		)
	}
	initVars := make(model.Vars)
	// decode the variables
	if len(vrs) != 0 {
		initVars, err = vars.Decode(c.log, vrs)
		if err != nil {
			return "", err
		}
	}
	var hasStartEvents bool
	// Test to see if all variables are present.
	vErr := forEachStartElement(wf, func(el *model.Element) error {
		hasStartEvents = true
		if el.OutputTransform != nil {
			for _, v := range el.OutputTransform {
				evs, err := expression.GetVariables(v)
				if err != nil {
					return err
				}
				for ev := range evs {
					if _, ok := initVars[ev]; !ok {
						return errors2.New("Workflow expects variable: " + ev)
					}
				}
			}
		}
		return nil
	})

	if vErr != nil {
		return "", vErr
	}

	ret := ""

	// Test to see if all variables are present.
	vErr = forEachTimedStartElement(wf, func(el *model.Element) error {
		if el.Type == "timedStartEvent" {
			timer := &model.WorkflowState{
				WorkflowId:   wfID,
				ElementId:    el.Id,
				UnixTimeNano: time.Now().UnixNano(),
				Timer: &model.WorkflowTimer{
					LastFired: 0,
					Count:     0,
				},
			}
			return c.ns.PublishWorkflowState(ctx, subj.SubjNS(messages.WorkflowTimedExecute, "default"), timer, 0)
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
				zap.String(keys.ParentInstanceElementID, parentElID),
				zap.String(keys.ParentWorkflowInstanceID, parentwfiID),
				zap.String(keys.WorkflowName, workflowName),
				zap.String(keys.WorkflowID, wfID),
			)
		}

		if vErr != nil {
			return "", vErr
		}

		// index the workflow
		els := common.ElementTable(wf)
		errs := make(chan error)
		wg := sync.WaitGroup{}

		// for each start element, launch a workflow thread
		startErr := forEachStartElement(wf, func(el *model.Element) error {
			wg.Add(1)

			trackingID := ksuid.New().String()
			state := &model.WorkflowState{
				Id:                 trackingID,
				WorkflowInstanceId: wfi.WorkflowInstanceId,
				WorkflowId:         wfID,
				ElementId:          el.Id,
				ElementType:        el.Type,
				Vars:               nil,
				LocalVars:          vrs,
			}
			err := vars.OutputVars(c.log, state, el)
			if err != nil {
				return err
			}

			// fire off the new workflow state
			if err := c.ns.PublishWorkflowState(ctx, messages.WorkflowInstanceExecute, state, 0); err != nil {
				errs <- c.engineErr(ctx, "failed to publish workflow state", err,
					zap.String(keys.ParentInstanceElementID, parentElID),
					zap.String(keys.ParentWorkflowInstanceID, parentwfiID),
					zap.String(keys.WorkflowName, workflowName),
					zap.String(keys.WorkflowID, wfID),
				)
			}

			// traverse all the outbound paths
			go func(el *model.Element) {
				defer wg.Done()

				if err := c.traverse(ctx, wfi, trackingID, el.Outbound, els, state.Vars); errors.IsWorkflowFatal(err) {
					c.log.Fatal("workflow fatally terminated whilst traversing", zap.String(keys.WorkflowInstanceID, wfi.WorkflowInstanceId), zap.String(keys.WorkflowID, wfi.WorkflowId), zap.Error(err), zap.String(keys.ElementName, el.Name))
					return
				} else if err != nil {
					errs <- fmt.Errorf("failed traversal to %v: %w", el.Outbound, err)
				}

			}(el)
			return nil
		})
		if startErr != nil {
			return "", startErr
		}
		// wait for all paths to be started
		wg.Wait()
		close(errs)
		if err := <-errs; err != nil {
			return "", c.engineErr(ctx, "failed initial traversal", err,
				zap.String(keys.ParentInstanceElementID, parentElID),
				zap.String(keys.ParentWorkflowInstanceID, parentwfiID),
				zap.String(keys.WorkflowName, workflowName),
				zap.String(keys.WorkflowID, wfID),
			)
		}
		ret = wfi.WorkflowInstanceId
	}
	return ret, nil
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
func (c *Engine) traverse(ctx context.Context, wfi *model.WorkflowInstance, parentTrackingId string, outbound *model.Targets, el map[string]*model.Element, v []byte) error {
	if outbound == nil {
		return nil
	}
	// Traverse along all outbound edges
	for _, t := range outbound.Target {
		ok := true
		// Evaluate conditions
		for _, ex := range t.Conditions {

			// TODO: Cache compilation.
			exVars, err := vars.Decode(c.log, v)
			if err != nil {
				return err
			}

			// evaluate the condition
			res, err := expression.Eval[bool](c.log, ex, exVars)
			if err != nil {
				return &errors.ErrWorkflowFatal{Err: err}
			}
			if !res {
				ok = false
				break
			}
		}

		target := el[t.Target]
		//Deal with timer events
		var embargo int
		if target.Type == "timerIntermediateCatchEvent" {
			// if the taget is an expression
			if target.Execute[0] == '=' {
				// unpack the variables
				exVars, err := vars.Decode(c.log, v)
				if err != nil {
					return err
				}
				// compile and execute the expression
				program, err := expr.Compile(target.Execute[1:], expr.Env(exVars))
				if err != nil {
					return err
				}
				res, err := expr.Run(program, exVars)
				if err != nil {
					return err
				}
				switch res.(type) {
				case int:
					// set the timer value
					embargo = int(time.Now().UnixNano() + res.(int64))
				default:
					return fmt.Errorf("delay did not evaluate to a 64 bit integer")
				}
			} else {
				p, err := strconv.Atoi(target.Execute)
				if err != nil {
					return err
				}
				embargo = int(time.Now().UnixNano() + int64(p))
			}

		}

		// If the conditions passed commit a traversal
		if ok {
			trackingId := ksuid.New().String()
			if err := c.ns.PublishWorkflowState(ctx, messages.WorkflowTraversalExecute, &model.WorkflowState{
				ElementType:        target.Type,
				ElementId:          target.Id,
				WorkflowId:         wfi.WorkflowId,
				WorkflowInstanceId: wfi.WorkflowInstanceId,
				Id:                 trackingId,
				ParentId:           parentTrackingId,
				Vars:               v,
			}, embargo); err != nil {
				c.log.Error("failed to publish workflow state", zap.Error(err))
				return err
			}

			if outbound.Exclusive {
				break
			}
		}
	}
	return nil
}

// activityProcessor handles the behaviour of each BPMN element
func (c *Engine) activityProcessor(ctx context.Context, traversal *model.WorkflowState, traverseOnly bool) error {
	// set the default state to be 'executing'
	state := model.CancellationState_Executing

	// get the corresponding workflow instance
	wfi, err := c.ns.GetWorkflowInstance(ctx, traversal.WorkflowInstanceId)
	if err == errors.ErrWorkflowInstanceNotFound || errors2.Is(err, nats.ErrKeyNotFound) {
		// if the workflow instance has been removed kill any activity and exit
		c.log.Warn("workflow instance not found, cancelling activity", zap.Error(err), zap.String(keys.WorkflowInstanceID, traversal.WorkflowInstanceId))
		return nil
	} else if err != nil {
		return c.engineErr(ctx, "failed to get workflow instance", err,
			zap.String(keys.WorkflowInstanceID, traversal.WorkflowInstanceId),
		)
	}

	// get the corresponding workflow definition
	process, err := c.ns.GetWorkflow(ctx, wfi.WorkflowId)
	if err != nil {
		return c.engineErr(ctx, "failed to get workflow", err,
			zap.String(keys.WorkflowInstanceID, wfi.WorkflowInstanceId),
			zap.String(keys.WorkflowID, wfi.WorkflowId),
		)
	}

	// create an indexed map of elements
	els := common.ElementTable(process)
	el := els[traversal.ElementId]

	// force traversal will not process the event, and will just traverse instead.
	if traverseOnly {
		el.Type = "forceTraversal"
	}

	trackingId := ksuid.New().String()

	// tell the world we are going to execute an activity
	if err := c.ns.PublishWorkflowState(ctx, messages.WorkflowActivityExecute, &model.WorkflowState{
		Id:                 trackingId,
		ElementType:        el.Type,
		ElementId:          traversal.ElementId,
		WorkflowInstanceId: traversal.WorkflowInstanceId,
		ParentId:           traversal.ParentId,
		WorkflowId:         wfi.WorkflowId,
		Vars:               traversal.Vars,
	}, 0); err != nil {
		return c.engineErr(ctx, "failed to publish workflow state", err, apErrFields(wfi.WorkflowInstanceId, wfi.WorkflowId, el.Id, el.Name, el.Type, process.Name)...)
	}
	// tell the world we have safely completed the traversal
	if err := c.ns.PublishWorkflowState(ctx, messages.WorkflowTraversalComplete, &model.WorkflowState{
		ElementType:        el.Type,
		ElementId:          traversal.ElementId,
		WorkflowId:         wfi.WorkflowId,
		WorkflowInstanceId: traversal.WorkflowInstanceId,
		Id:                 traversal.Id,
		ParentId:           traversal.ParentId,
		Vars:               traversal.Vars,
	}, 0); err != nil {
		return c.engineErr(ctx, "failed to publish workflow state", err, apErrFields(wfi.WorkflowInstanceId, wfi.WorkflowId, el.Id, el.Name, el.Type, process.Name)...)
	}

	// process any supported ebents
	switch el.Type {
	case "serviceTask":
		stID, err := c.ns.GetServiceTaskRoutingKey(el.Execute)
		if err != nil {
			return err
		}
		if err := c.startJob(ctx, messages.WorkflowJobServiceTaskExecute+"."+stID, wfi.WorkflowId, traversal.WorkflowInstanceId, traversal.ParentId, el, "", traversal.Vars); err != nil {
			return c.engineErr(ctx, "failed to start srvice task job", err, apErrFields(wfi.WorkflowInstanceId, wfi.WorkflowId, el.Id, el.Name, el.Type, process.Name)...)
		}
	case "userTask":
		if err := c.startJob(ctx, messages.WorkflowJobUserTaskExecute, wfi.WorkflowId, traversal.WorkflowInstanceId, traversal.ParentId, el, "", traversal.Vars); err != nil {
			return c.engineErr(ctx, "failed to start user task job", err, apErrFields(wfi.WorkflowInstanceId, wfi.WorkflowId, el.Id, el.Name, el.Type, process.Name)...)
		}
	case "manualTask":
		if err := c.startJob(ctx, messages.WorkflowJobManualTaskExecute, wfi.WorkflowId, traversal.WorkflowInstanceId, traversal.ParentId, el, "", traversal.Vars); err != nil {
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
		sendMsgId, err := c.ns.GetMessageSenderRoutingKey(wf.Name, wf.Messages[ix].Name)
		if err != nil {
			return err
		}
		if err := c.startJob(ctx, messages.WorkflowJobSendMessageExecute+"."+sendMsgId, wfi.WorkflowId, traversal.WorkflowInstanceId, traversal.ParentId, el, wf.Messages[ix].Execute, traversal.Vars); err != nil {
			return c.engineErr(ctx, "failed to start message job", err, apErrFields(wfi.WorkflowInstanceId, wfi.WorkflowId, el.Id, el.Name, el.Type, process.Name)...)
		}
	case "callActivity":
		if _, err := c.launch(ctx, el.Execute, traversal.Vars, traversal.WorkflowInstanceId, el.Id); err != nil {
			return c.engineErr(ctx, "failed to launch child workflow", err, apErrFields(wfi.WorkflowInstanceId, wfi.WorkflowId, el.Id, el.Name, el.Type, process.Name)...)
		}
	case "messageIntermediateCatchEvent":
		if err := c.awaitMessage(ctx, wfi.WorkflowId, traversal.WorkflowInstanceId, traversal.ParentId, el, traversal.Vars); err != nil {
			return c.engineErr(ctx, "failed to await message", err, apErrFields(wfi.WorkflowInstanceId, wfi.WorkflowId, el.Id, el.Name, el.Type, process.Name)...)
		}
	case "endEvent":
		if wfi.ParentWorkflowInstanceId != nil && *wfi.ParentWorkflowInstanceId != "" {
			if err := c.returnBack(ctx, wfi.WorkflowInstanceId, *wfi.ParentWorkflowInstanceId, *wfi.ParentElementId, traversal.Vars); err != nil {
				return c.engineErr(ctx, "failed to return to originator workflow", err, apErrFields(wfi.WorkflowInstanceId, wfi.WorkflowId, el.Id, el.Name, el.Type, process.Name, zap.String(keys.ParentWorkflowInstanceID, *wfi.ParentWorkflowInstanceId))...)
			}

		} else {
			if len(el.Errors) == 0 {
				state = model.CancellationState_Completed
			} else {
				state = model.CancellationState_Errored

			}
		}
	default:
		// if we don't support the event, just traverse to the next element
		if err := c.traverse(ctx, wfi, trackingId, el.Outbound, els, traversal.Vars); errors.IsWorkflowFatal(err) {
			c.log.Fatal("workflow fatally terminated whilst traversing", zap.String(keys.WorkflowInstanceID, wfi.WorkflowInstanceId), zap.String(keys.WorkflowID, wfi.WorkflowId), zap.Error(err), zap.String(keys.ElementName, el.Name))
			return nil
		} else if err != nil {
			return c.engineErr(ctx, "failed to return to traverse", err, apErrFields(wfi.WorkflowInstanceId, wfi.WorkflowId, el.Id, el.Name, el.Type, process.Name)...)
		}
	}

	// tell the world that we processed the activity
	if err := c.ns.PublishWorkflowState(ctx, messages.WorkflowActivityComplete, &model.WorkflowState{
		Id:                 trackingId,
		ElementType:        el.Type,
		ElementId:          traversal.ElementId,
		WorkflowId:         wfi.WorkflowId,
		WorkflowInstanceId: traversal.WorkflowInstanceId,
		ParentId:           wfi.WorkflowInstanceId,
		State:              state,
		Error:              el.Error,
		Vars:               traversal.Vars,
	}, 0); err != nil {
		return c.engineErr(ctx, "failed to publish workflow state", err, apErrFields(wfi.WorkflowInstanceId, wfi.WorkflowId, el.Id, el.Name, el.Type, process.Name)...)
	}

	// if the workflow is complete, send an instance complete message to trigger tidy up
	if state == model.CancellationState_Completed || state == model.CancellationState_Errored || state == model.CancellationState_Terminated {
		if err := c.ns.PublishWorkflowState(ctx, messages.WorkflowInstanceComplete, &model.WorkflowState{
			Id:                 wfi.WorkflowInstanceId,
			WorkflowId:         wfi.WorkflowId,
			WorkflowInstanceId: traversal.WorkflowInstanceId,
			ElementId:          traversal.ElementId,
			ElementType:        el.Type,
			Error:              el.Error,
			State:              state,
		}, 0); err != nil {
			return c.engineErr(ctx, "failed to publish workflow state", err, apErrFields(wfi.WorkflowInstanceId, wfi.WorkflowId, el.Id, el.Name, el.Type, process.Name)...)
		}
	}
	return nil
}

// apErrFields writes out the common error fields for an application error
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

// returnBack is executed when a workflow sub-process is complete
func (c *Engine) returnBack(ctx context.Context, wfiID string, parentwfiID string, parentElID string, vars []byte) error {
	pwfi, err := c.ns.GetWorkflowInstance(ctx, parentwfiID)
	if err == errors.ErrWorkflowInstanceNotFound {
		c.log.Warn("parent workflow instance not found, cancelling return to caller", zap.Error(err), zap.String(keys.ParentWorkflowInstanceID, parentwfiID))
		return nil
	} else if err != nil {
		return fmt.Errorf("failed to fetch workflow instance for return back: %w", err)
	}
	pwf, err := c.ns.GetWorkflow(ctx, pwfi.WorkflowId)
	if err != nil {
		return c.engineErr(ctx, "failed to fetch return workflow", err,
			zap.String(keys.ParentWorkflowInstanceID, parentwfiID),
			zap.String(keys.WorkflowInstanceID, wfiID),
			zap.String(keys.ElementID, parentElID),
		)
	}
	index := common.ElementTable(pwf)
	el := index[parentElID]
	if err = c.traverse(ctx, pwfi, "", el.Outbound, index, vars); errors.IsWorkflowFatal(err) {
		c.log.Fatal("workflow fatally terminated whilst traversing", zap.String(keys.WorkflowInstanceID, wfiID), zap.String(keys.ParentWorkflowInstanceID, parentwfiID), zap.Error(err), zap.String(keys.ElementName, el.Name))
		return nil
	} else if err != nil {
		return c.engineErr(ctx, "failed to traverse", err,
			zap.String(keys.ElementName, el.Name),
			zap.String(keys.ElementID, el.Id),
			zap.String(keys.ElementType, el.Type),
			zap.String(keys.WorkflowInstanceID, wfiID),
			zap.String(keys.ParentWorkflowInstanceID, parentwfiID),
		)
	}
	return nil
}

// completeJobProcessor processes completed serviceTasks
func (c *Engine) completeJobProcessor(ctx context.Context, jobID string, vars []byte) error {
	// get the job associated with the trackingID
	job, err := c.ns.GetJob(ctx, jobID)
	if err != nil {
		c.log.Warn("failed to locate job", zap.Error(err),
			zap.String(keys.JobID, jobID),
		)
		return nil
	}

	// get the relevant workflow instance
	wfi, err := c.ns.GetWorkflowInstance(ctx, job.WorkflowInstanceId)
	if err == errors.ErrWorkflowInstanceNotFound {
		// if the instance has been deleted quash this activity
		c.log.Warn("workflow instance not found, cancelling job processing", zap.Error(err), zap.String(keys.WorkflowInstanceID, job.WorkflowInstanceId))
		return nil
	} else if err != nil {
		c.log.Warn("failed to get workflow instance for job", zap.Error(err),
			zap.String(keys.JobType, job.ElementType),
			zap.String(keys.JobID, jobID),
		)
		return err
	}

	// get the relevant workflow
	wf, err := c.ns.GetWorkflow(ctx, wfi.WorkflowId)
	if err != nil {
		return c.engineErr(ctx, "failed to fetch job workflow", err,
			zap.String(keys.JobType, job.ElementType),
			zap.String(keys.JobID, jobID),
			zap.String(keys.WorkflowInstanceID, wfi.WorkflowInstanceId),
			zap.String(keys.WorkflowID, wfi.WorkflowId),
		)
	}

	// build element table
	els := common.ElementTable(wf)
	el := els[job.ElementId]

	// traverse to next element
	if err := c.traverse(ctx, wfi, jobID, el.Outbound, els, vars); errors.IsWorkflowFatal(err) {
		c.log.Fatal("workflow fatally terminated whilst traversing", zap.String(keys.WorkflowInstanceID, wfi.WorkflowInstanceId), zap.String(keys.WorkflowID, wfi.WorkflowId), zap.Error(err), zap.String(keys.ElementName, el.Name))
		return nil
	} else if err != nil {
		return c.engineErr(ctx, "failed to launch traversal", err,
			zap.String(keys.ElementName, el.Name),
			zap.String(keys.ElementID, el.Id),
			zap.String(keys.ElementType, el.Type),
			zap.String(keys.JobType, job.ElementType),
			zap.String(keys.JobID, jobID),
			zap.String(keys.WorkflowInstanceID, wfi.WorkflowInstanceId),
			zap.String(keys.WorkflowID, wfi.WorkflowId),
		)
	}
	return nil
}

// startJob launches a user/service task
func (c *Engine) startJob(ctx context.Context, subject, wfID, wfiID, parentTrackingID string, el *model.Element, condition string, v []byte) error {
	trackingId := ksuid.New()
	job := &model.WorkflowState{
		Id:                 trackingId.String(),
		ParentId:           parentTrackingID,
		WorkflowId:         wfID,
		WorkflowInstanceId: wfiID,
		Vars:               v,
		ElementType:        el.Type,
		ElementId:          el.Id,
		Error:              el.Error,
		Execute:            &el.Execute,
		Condition:          &condition,
	}
	err := vars.InputVars(c.log, job, el)
	if err != nil {
		return err
	}
	vx, err := vars.Decode(c.log, v)
	if err != nil {
		return err
	}

	// if this is a user task, finf out who can perfoem it
	if el.Type == "userTask" {
		owners, err := c.evaluateOwners(el.Candidates, vx)
		if err != nil {
			return err
		}
		groups, err := c.evaluateOwners(el.CandidateGroups, vx)
		if err != nil {
			return err
		}

		job.Owners = owners
		job.Groups = groups
	}

	// create the job
	jobId, err := c.ns.CreateJob(ctx, job)

	if err != nil {
		return c.engineErr(ctx, "failed to start manual task", err,
			zap.String(keys.ElementName, el.Name),
			zap.String(keys.ElementID, el.Id),
			zap.String(keys.ElementType, el.Type),
			zap.String(keys.JobType, job.ElementType),
			zap.String(keys.JobID, job.Id),
			zap.String(keys.WorkflowInstanceID, wfiID),
		)
	}
	job.Id = jobId
	// finally tell the engine that the job is ready for a client
	return c.ns.PublishWorkflowState(ctx, subj.SubjNS(subject, "default"), job, 0)
}

// evaluateOwners builds a list of groups
func (c *Engine) evaluateOwners(owners string, vars model.Vars) ([]string, error) {
	jobGroups := make([]string, 0)
	groups, err := expression.Eval[interface{}](c.log, owners, vars)
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
		id, err := c.ns.OwnerId(v)
		if err != nil {
			return nil, err
		}
		jobGroups[i] = id
	}
	return jobGroups, nil
}

func (c *Engine) engineErr(_ context.Context, msg string, err error, z ...zap.Field) error {
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

// CancelWorkflowInstance will cancel a workflow instance with a reason
func (c *Engine) CancelWorkflowInstance(ctx context.Context, id string, state model.CancellationState, wfError *model.Error) error {
	if state == model.CancellationState_Executing {
		return errors2.New("executing is an invalid cancellation state")
	}
	return c.ns.DestroyWorkflowInstance(ctx, id, state, wfError)
}

// awaitMessage signals that the workflow instance will resume after a message is received
func (c *Engine) awaitMessage(ctx context.Context, wfID string, wfiID string, parentTrackingID string, el *model.Element, vars []byte) error {
	trackingId := ksuid.New().String()
	awaitMsg := &model.WorkflowState{
		WorkflowId:         wfID,
		WorkflowInstanceId: wfiID,
		ElementId:          el.Id,
		ElementType:        el.Type,
		Error:              el.Error,
		Id:                 trackingId,
		Execute:            &el.Execute,
		Condition:          &el.Msg,
		Vars:               vars,
		ParentId:           parentTrackingID,
	}

	err := c.ns.AwaitMsg(ctx, awaitMsg)
	if err != nil {
		return c.engineErr(ctx, "failed to await message", err,
			zap.String(keys.ElementName, el.Name),
			zap.String(keys.ElementID, el.Id),
			zap.String(keys.ElementType, el.Type),
			zap.String(keys.JobType, awaitMsg.ElementType),
			zap.String(keys.WorkflowInstanceID, wfiID),
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
	els := common.ElementTable(wf)
	if err = c.traverse(ctx, wfi, state.ParentId, els[state.ElementId].Outbound, els, state.Vars); errors.IsWorkflowFatal(err) {
		c.log.Fatal("workflow fatally terminated whilst traversing", zap.String(keys.WorkflowInstanceID, wfi.WorkflowInstanceId), zap.String(keys.WorkflowID, wfi.WorkflowId), zap.Error(err), zap.String(keys.ElementID, state.ElementId))
		return nil
	} else {
		return err
	}
}

func (c *Engine) CompleteManualTask(ctx context.Context, trackingID string, newvars []byte) error {
	fmt.Println("Complete Manual Task")
	job, err := c.ns.GetJob(ctx, trackingID)
	if err != nil {
		return err
	}
	el, err := c.ns.GetElement(ctx, job)
	if err != nil {
		return &errors.ErrWorkflowFatal{Err: err}
	}
	job.LocalVars = newvars
	err = vars.CheckVars(c.log, job, el)
	if err != nil {
		return &errors.ErrWorkflowFatal{Err: err}
	}
	err = vars.OutputVars(c.log, job, el)
	if err != nil {
		return &errors.ErrWorkflowFatal{Err: err}
	}
	if err := c.ns.PublishWorkflowState(ctx, messages.WorkflowJobManualTaskComplete, job, 0); err != nil {
		return err
	}
	return nil
}

func (c *Engine) CompleteServiceTask(ctx context.Context, trackingID string, newvars []byte) error {
	job, err := c.ns.GetJob(ctx, trackingID)
	if err != nil {
		return err
	}
	el, err := c.ns.GetElement(ctx, job)
	if err != nil {
		return &errors.ErrWorkflowFatal{Err: err}
	}
	job.LocalVars = newvars
	err = vars.CheckVars(c.log, job, el)
	if err != nil {
		return &errors.ErrWorkflowFatal{Err: err}
	}
	err = vars.OutputVars(c.log, job, el)
	if err != nil {
		return &errors.ErrWorkflowFatal{Err: err}
	}
	if err := c.ns.PublishWorkflowState(ctx, messages.WorkflowJobServiceTaskComplete, job, 0); err != nil {
		return err
	}
	return nil
}

func (c *Engine) CompleteSendMessage(ctx context.Context, trackingID string, newvars []byte) error {
	job, err := c.ns.GetJob(ctx, trackingID)
	if err != nil {
		return err
	}
	el, err := c.ns.GetElement(ctx, job)
	if err != nil {
		return &errors.ErrWorkflowFatal{Err: err}
	}
	job.LocalVars = newvars
	err = vars.CheckVars(c.log, job, el)
	if err != nil {
		return &errors.ErrWorkflowFatal{Err: err}
	}
	err = vars.OutputVars(c.log, job, el)
	if err != nil {
		return &errors.ErrWorkflowFatal{Err: err}
	}
	if err := c.ns.PublishWorkflowState(ctx, messages.WorkflowJobSendMessageComplete, job, 0); err != nil {
		return err
	}
	return nil
}

func (c *Engine) CompleteUserTask(ctx context.Context, trackingID string, newvars []byte) error {
	fmt.Println("Complete User Task")
	job, err := c.ns.GetJob(ctx, trackingID)
	if err != nil {
		return err
	}
	el, err := c.ns.GetElement(ctx, job)
	if err != nil {
		return &errors.ErrWorkflowFatal{Err: err}
	}
	job.LocalVars = newvars
	err = vars.CheckVars(c.log, job, el)
	if err != nil {
		return &errors.ErrWorkflowFatal{Err: err}
	}
	err = vars.OutputVars(c.log, job, el)
	if err != nil {
		return &errors.ErrWorkflowFatal{Err: err}
	}
	if err := c.ns.PublishWorkflowState(ctx, messages.WorkflowJobUserTaskComplete, job, 0); err != nil {
		return err
	}
	if err := c.ns.CloseUserTask(ctx, trackingID); err != nil {
		return err
	}
	return nil
}
