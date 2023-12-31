package workflow

import (
	"context"
	errors2 "errors"
	"fmt"
	"github.com/nats-io/nats.go"
	"github.com/segmentio/ksuid"
	"gitlab.com/shar-workflow/shar/client/parser"
	"gitlab.com/shar-workflow/shar/common"
	"gitlab.com/shar-workflow/shar/common/element"
	"gitlab.com/shar-workflow/shar/common/expression"
	"gitlab.com/shar-workflow/shar/common/logx"
	"gitlab.com/shar-workflow/shar/common/subj"
	"gitlab.com/shar-workflow/shar/model"
	"gitlab.com/shar-workflow/shar/server/errors"
	"gitlab.com/shar-workflow/shar/server/errors/keys"
	"gitlab.com/shar-workflow/shar/server/messages"
	"gitlab.com/shar-workflow/shar/server/services"
	"gitlab.com/shar-workflow/shar/server/services/storage"
	"gitlab.com/shar-workflow/shar/server/vars"
	"golang.org/x/exp/slog"
	"google.golang.org/protobuf/proto"
	"strconv"
	"time"
)

// Engine contains the workflow processing functions
type Engine struct {
	closing chan struct{}
	ns      NatsService
}

// New returns an instance of the core workflow engine.
func New(ns NatsService) (*Engine, error) {
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

	// Set traversal function
	c.ns.SetTraversalProvider(c.traverse)

	// Set the completed activity processor
	c.ns.SetCompleteActivityProcessor(c.activityCompleteProcessor)

	// Set the completed activity processor
	c.ns.SetCompleteActivity(c.completeActivity)

	c.ns.SetMessageProcessor(c.timedExecuteProcessor)

	c.ns.SetLaunchFunc(c.launchProcessor)

	c.ns.SetAbort(c.abortProcessor)

	if err := c.ns.StartProcessing(ctx); err != nil {
		return fmt.Errorf("start processing: %w", err)
	}
	return nil
}

// LoadWorkflow loads a model.Process describing a workflow into the engine ready for execution.
func (c *Engine) LoadWorkflow(ctx context.Context, model *model.Workflow) (string, error) {
	// Store the workflow model and return an ID
	wfID, err := c.ns.StoreWorkflow(ctx, model)
	if err != nil {
		return "", fmt.Errorf("store workflow: %w", err)
	}
	return wfID, nil
}

// Launch starts a new instance of a workflow and returns a workflow instance Id.
func (c *Engine) Launch(ctx context.Context, workflowName string, vars []byte) (string, string, error) {
	return c.launch(ctx, workflowName, []string{}, vars, "", "")
}

// launch contains the underlying logic to start a workflow.  It is also called to spawn new instances of child workflows.
func (c *Engine) launch(ctx context.Context, workflowName string, ID common.TrackingID, vrs []byte, parentpiID string, parentElID string) (string, string, error) {
	var reterr error
	ctx, log := logx.ContextWith(ctx, "engine.launch")
	// get the last ID of the workflow
	wfID, err := c.ns.GetLatestVersion(ctx, workflowName)
	if err != nil {
		reterr = c.engineErr(ctx, "get latest version of workflow", err,
			slog.String(keys.ParentInstanceElementID, parentElID),
			slog.String(keys.ParentProcessInstanceID, parentpiID),
			slog.String(keys.WorkflowName, workflowName),
		)
		return "", "", reterr
	}

	// get the last version of the workflow
	wf, err := c.ns.GetWorkflow(ctx, wfID)
	if err != nil {
		reterr = c.engineErr(ctx, "get workflow", err,
			slog.String(keys.ParentInstanceElementID, parentElID),
			slog.String(keys.ParentProcessInstanceID, parentpiID),
			slog.String(keys.WorkflowName, workflowName),
			slog.String(keys.WorkflowID, wfID),
		)
		return "", "", reterr
	}

	wfi, err := c.ns.CreateWorkflowInstance(ctx, &model.WorkflowInstance{
		WorkflowId:              wfID,
		ParentProcessInstanceId: &parentpiID,
		ParentElementId:         &parentElID,
		WorkflowName:            wf.Name,
	})

	if err != nil {
		reterr = c.engineErr(ctx, "create workflow instance", err,
			slog.String(keys.ParentInstanceElementID, parentElID),
			slog.String(keys.ParentProcessInstanceID, parentpiID),
			slog.String(keys.WorkflowName, workflowName),
			slog.String(keys.WorkflowID, wfID),
		)
		return "", "", reterr
	}

	defer func() {
		if reterr != nil {
			c.rollBackLaunch(ctx, wfi)
		}
	}()

	wiState := &model.WorkflowState{
		WorkflowInstanceId: wfi.WorkflowInstanceId,
		WorkflowId:         wfID,
		WorkflowName:       workflowName,
		Vars:               vrs,
		Id:                 []string{wfi.WorkflowInstanceId},
	}

	// fire off the new workflow state
	if err := c.ns.PublishWorkflowState(ctx, messages.WorkflowInstanceExecute, wiState); err != nil {
		reterr = c.engineErr(ctx, "publish workflow instance execute", err,
			slog.String(keys.WorkflowName, workflowName),
			slog.String(keys.WorkflowID, wfID),
		)
		return "", "", reterr
	}

	testVars, err := vars.Decode(ctx, vrs)
	if err != nil {
		return "", "", fmt.Errorf("decode variables during launch: %w", err)
	}
	for prName, pr := range wf.Process {
		var hasStartEvents bool
		// Test to see if all variables are present.
		vErr := forEachStartElement(pr, func(el *model.Element) error {
			hasStartEvents = true
			if el.OutputTransform != nil {
				for _, v := range el.OutputTransform {
					evs, err := expression.GetVariables(v)
					if err != nil {
						return fmt.Errorf("extract variables from workflow during launch: %w", err)
					}
					for ev := range evs {
						if _, ok := testVars[ev]; !ok {
							return fmt.Errorf("workflow expects variable '%s': %w", ev, errors.ErrExpectedVar)
						}
					}
				}
			}
			return nil
		})

		if vErr != nil {
			reterr = fmt.Errorf("initialize all workflow start events: %w", vErr)
			return "", "", reterr
		}

		// Start all timed start events.
		vErr = forEachTimedStartElement(pr, func(el *model.Element) error {
			if el.Type == element.TimedStartEvent {
				timer := &model.WorkflowState{
					Id:                 ID.Push(ksuid.New().String()),
					WorkflowId:         wfID,
					WorkflowInstanceId: wfi.WorkflowInstanceId,
					ElementId:          el.Id,
					UnixTimeNano:       time.Now().UnixNano(),
					Timer: &model.WorkflowTimer{
						LastFired: 0,
						Count:     0,
					},
					Vars:         vrs,
					WorkflowName: wf.Name,
					ProcessName:  pr.Name,
				}
				if err := c.ns.PublishWorkflowState(ctx, subj.NS(messages.WorkflowTimedExecute, "default"), timer); err != nil {
					return fmt.Errorf("publish workflow timed execute: %w", err)
				}
				return nil
			}
			return nil
		})

		if vErr != nil {
			reterr = fmt.Errorf("initialize all workflow timed start events: %w", vErr)
			return "", "", reterr
		}

		if hasStartEvents {

			pi, err := c.ns.CreateProcessInstance(ctx, wfi.WorkflowInstanceId, parentpiID, parentElID, pr.Name)
			if err != nil {
				reterr = fmt.Errorf("launch failed to create new process instance: %w", err)
				return "", "", reterr
			}

			errs := make(chan error)

			// for each start element, launch a workflow thread
			startErr := forEachStartElement(pr, func(el *model.Element) error {
				trackingID := ID.Push(pi.ProcessInstanceId).Push(ksuid.New().String())
				exec := &model.WorkflowState{
					ElementType:        el.Type,
					ElementId:          el.Id,
					WorkflowId:         wfi.WorkflowId,
					WorkflowInstanceId: wfi.WorkflowInstanceId,
					Id:                 trackingID,
					Vars:               vrs,
					WorkflowName:       wf.Name,
					ProcessName:        prName,
					ProcessInstanceId:  pi.ProcessInstanceId,
				}
				if err := c.ns.PublishWorkflowState(ctx, subj.NS(messages.WorkflowProcessExecute, "default"), exec); err != nil {
					return fmt.Errorf("publish workflow timed process execute: %w", err)
				}
				if err := c.ns.RecordHistoryProcessStart(ctx, exec); err != nil {
					log.Error("start events record process start", err)
					return fmt.Errorf("publish initial traversal: %w", err)
				}
				if err := c.ns.PublishWorkflowState(ctx, messages.WorkflowTraversalExecute, exec); err != nil {
					log.Error("publish initial traversal", err)
					return fmt.Errorf("publish initial traversal: %w", err)
				}
				return nil
			})
			if startErr != nil {
				reterr = startErr
				return "", "", startErr
			}
			// wait for all paths to be started
			close(errs)
			if err := <-errs; err != nil {
				reterr = c.engineErr(ctx, "initial traversal", err,
					slog.String(keys.ParentInstanceElementID, parentElID),
					slog.String(keys.ParentProcessInstanceID, parentpiID),
					slog.String(keys.WorkflowName, workflowName),
					slog.String(keys.WorkflowID, wfID),
				)
				return "", "", reterr
			}
		}
	}
	return wfi.WorkflowInstanceId, wfID, nil
}

func (c *Engine) rollBackLaunch(ctx context.Context, wfi *model.WorkflowInstance) {
	log := logx.FromContext(ctx)
	log.Info("rolling back workflow launch")
	err := c.ns.PublishWorkflowState(ctx, messages.WorkflowInstanceAbort, &model.WorkflowState{
		Id:                 []string{wfi.WorkflowInstanceId},
		WorkflowInstanceId: wfi.WorkflowInstanceId,
		WorkflowName:       wfi.WorkflowName,
		WorkflowId:         wfi.WorkflowId,
		State:              model.CancellationState_terminated,
	})
	if err != nil {
		log.Error("workflow fatally terminated, however the termination signal could not be sent", err)
	}
}

// forEachStartElement finds all start elements for a given process and executes a function on the element.
func forEachStartElement(pr *model.Process, fn func(element *model.Element) error) error {
	for _, i := range pr.Elements {
		if i.Type == element.StartEvent {
			err := fn(i)
			if err != nil {
				return fmt.Errorf("start event execution: %w", err)
			}
		}
	}
	return nil
}

// forEachStartElement finds all start elements for a given process and executes a function on the element.
func forEachTimedStartElement(pr *model.Process, fn func(element *model.Element) error) error {
	for _, i := range pr.Elements {
		if i.Type == element.TimedStartEvent {
			err := fn(i)
			if err != nil {
				return fmt.Errorf("timed start event execution: %w", err)
			}
		}
	}
	return nil
}

// traverse traverses all outbound connections provided the conditions passed if available.
func (c *Engine) traverse(ctx context.Context, pr *model.ProcessInstance, trackingID common.TrackingID, outbound *model.Targets, el map[string]*model.Element, state *model.WorkflowState) error {
	ctx, log := logx.ContextWith(ctx, "engine.traverse")
	if outbound == nil {
		return nil
	}
	commit := make(map[string]string, len(outbound.Target))
	// Traverse along all outbound edges
	for _, t := range outbound.Target {
		ws := proto.Clone(state).(*model.WorkflowState)
		// Evaluate conditions
		ok := true
		for _, ex := range t.Conditions {

			// TODO: Cache compilation.
			exVars, err := vars.Decode(ctx, ws.Vars)
			if err != nil {
				return fmt.Errorf("decode variables for condition evaluation: %w", err)
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
		if ok {
			commit[t.Id] = t.Target
		}
	}

	elem := el[state.ElementId]

	reciprocatedDivergentGateway := elem.Type == element.Gateway && elem.Gateway.Direction == model.GatewayDirection_divergent && elem.Gateway.ReciprocalId != ""

	divergentGatewayReciprocalInstanceId := ksuid.New().String()

	// Check traversals from a reciprocated divergent gateway
	if reciprocatedDivergentGateway {

		keys := make([]string, 0, len(commit))
		for k := range commit {
			keys = append(keys, k)
		}
		if state.GatewayExpectations == nil {
			state.GatewayExpectations = make(map[string]*model.GatewayExpectations)
		}
		state.GatewayExpectations[divergentGatewayReciprocalInstanceId] = &model.GatewayExpectations{
			ExpectedPaths: keys,
		}
	}

	for branchID, elID := range commit {
		ws := proto.Clone(state).(*model.WorkflowState)
		newID := ksuid.New().String()
		tID := trackingID.Push(newID)
		target := el[elID]
		ws.Id = tID
		ws.ElementType = target.Type
		ws.ElementId = target.Id

		// Check traversals that lead to solitary convergent gateways
		if target.Type == element.Gateway && target.Gateway.Direction == model.GatewayDirection_convergent && target.Gateway.ReciprocalId == "" {
			if ws.SatisfiesGatewayExpectation == nil {
				ws.SatisfiesGatewayExpectation = make(map[string]*model.SatisfiesGateway)
			}
			if _, ok := ws.SatisfiesGatewayExpectation[target.Id]; !ok {
				ws.SatisfiesGatewayExpectation[target.Id] = &model.SatisfiesGateway{InstanceTracking: make([]string, 0)}
			}
			ws.SatisfiesGatewayExpectation[target.Id].InstanceTracking = append(ws.SatisfiesGatewayExpectation[target.Id].InstanceTracking, "-,"+branchID)
		}

		if reciprocatedDivergentGateway {
			if ws.SatisfiesGatewayExpectation == nil {
				ws.SatisfiesGatewayExpectation = make(map[string]*model.SatisfiesGateway)
			}
			if _, ok := ws.SatisfiesGatewayExpectation[elem.Gateway.ReciprocalId]; !ok {
				ws.SatisfiesGatewayExpectation[elem.Gateway.ReciprocalId] = &model.SatisfiesGateway{InstanceTracking: make([]string, 0)}
			}
			ws.SatisfiesGatewayExpectation[elem.Gateway.ReciprocalId].InstanceTracking = append(ws.SatisfiesGatewayExpectation[elem.Gateway.ReciprocalId].InstanceTracking, divergentGatewayReciprocalInstanceId+","+branchID)
		}

		if err := c.ns.PublishWorkflowState(ctx, messages.WorkflowTraversalExecute, ws); err != nil {
			log.Error("publish workflow state", err)
			return fmt.Errorf("publish workflow state: %w", err)
		}
	}
	return nil
}

// activityStartProcessor handles the behaviour of each BPMN element
func (c *Engine) activityStartProcessor(ctx context.Context, newActivityID string, traversal *model.WorkflowState, traverseOnly bool) error {
	ctx, log := logx.ContextWith(ctx, "engine.activityStartProcessor")
	// set the default status to be 'executing'
	status := model.CancellationState_executing

	// get the corresponding process instance
	pi, err := c.ns.GetProcessInstance(ctx, traversal.ProcessInstanceId)
	if errors2.Is(err, errors.ErrProcessInstanceNotFound) || errors2.Is(err, nats.ErrKeyNotFound) {
		// if the workflow instance has been removed kill any activity and exit
		log.Warn("process instance not found, cancelling activity", err, slog.String(keys.WorkflowInstanceID, traversal.WorkflowInstanceId))
		return nil
	} else if err != nil {
		return c.engineErr(ctx, "get process instance", err,
			slog.String(keys.ProcessInstanceID, traversal.ProcessInstanceId),
		)
	}

	// get the corresponding workflow instance so we can cancel
	_, err = c.ns.GetWorkflowInstance(ctx, traversal.WorkflowInstanceId)
	if errors2.Is(err, errors.ErrWorkflowInstanceNotFound) || errors2.Is(err, nats.ErrKeyNotFound) {
		// if the workflow instance has been removed kill any activity and exit
		log.Warn("workflow instance not found, cancelling activity", err, slog.String(keys.WorkflowInstanceID, traversal.WorkflowInstanceId))
		return nil
	} else if err != nil {
		return c.engineErr(ctx, "get workflow instance", err,
			slog.String(keys.WorkflowInstanceID, traversal.WorkflowInstanceId),
		)
	}

	// get the corresponding workflow definition
	process, err := c.ns.GetWorkflow(ctx, pi.WorkflowId)
	if err != nil {
		return c.engineErr(ctx, "get workflow", err,
			slog.String(keys.WorkflowInstanceID, pi.WorkflowInstanceId),
			slog.String(keys.WorkflowID, pi.WorkflowId),
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

	newState := common.CopyWorkflowState(traversal)
	newState.Id = activityID
	// tell the world we are going to execute an activity
	if err := c.ns.PublishWorkflowState(ctx, messages.WorkflowActivityExecute, newState); err != nil {
		return c.engineErr(ctx, "publish workflow status", err, apErrFields(pi.WorkflowInstanceId, pi.WorkflowId, el.Id, el.Name, el.Type, process.Name)...)
	}
	// log this with history
	if err := c.ns.RecordHistoryActivityExecute(ctx, newState); err != nil {
		return c.engineErr(ctx, "publish process history", err, apErrFields(pi.WorkflowInstanceId, pi.WorkflowId, el.Id, el.Name, el.Type, process.Name)...)
	}

	// tell the world we have safely completed the traversal
	if err := c.ns.PublishWorkflowState(ctx, messages.WorkflowTraversalComplete, traversal); err != nil {
		return c.engineErr(ctx, "publish traversal status", err, apErrFields(pi.WorkflowInstanceId, pi.WorkflowId, el.Id, el.Name, el.Type, process.Name)...)
	}

	//Start any timers
	if el.BoundaryTimer != nil || len(el.BoundaryTimer) > 0 {
		for _, i := range el.BoundaryTimer {
			timerState := common.CopyWorkflowState(newState)
			timerState.Execute = &i.Target
			timerState.UnixTimeNano = time.Now().UnixNano()
			timerState.Timer = &model.WorkflowTimer{LastFired: 0, Count: 0}
			v, err := vars.Decode(ctx, traversal.Vars)
			if err != nil {
				return fmt.Errorf("decode boundary timer variable: %w", err)
			}
			res, err := expression.EvalAny(ctx, i.Duration, v)
			if err != nil {
				return fmt.Errorf("evaluate boundary timer expression: %w", err)
			}
			ut := time.Now()
			switch x := res.(type) {
			case int:
				ut = ut.Add(time.Duration(x))
			case string:
				dur, err := parser.ParseISO8601(x)
				if err != nil {
					return fmt.Errorf("parse ISO8601 boundary timer value: %w", err)
				}
				ut = dur.Shift(ut)
			}
			err = c.ns.PublishWorkflowState(ctx, subj.NS(messages.WorkflowElementTimedExecute, "default"), timerState, storage.WithEmbargo(int(ut.UnixNano())))
			if err != nil {
				return fmt.Errorf("publish timed execute during activity start: %w", err)
			}
		}
	}

	// process any supported events
	switch el.Type {
	case element.StartEvent:
		initVars := make([]byte, 0)
		err := vars.OutputVars(ctx, traversal.Vars, &initVars, el.OutputTransform)
		if err != nil {
			return fmt.Errorf("get output vars for start event: %w", err)
		}
		traversal.Vars = initVars
		newState.State = status
		newState.Vars = traversal.Vars
		if err := c.completeActivity(ctx, newState); err != nil {
			return fmt.Errorf("start event complete activity: %w", err)
		}
	case element.Gateway:
		completeActivityState := common.CopyWorkflowState(newState)
		completeActivityState.State = status
		//
		if el.Gateway.Direction == model.GatewayDirection_convergent {
			if err := c.ns.PublishWorkflowState(ctx, messages.WorkflowJobGatewayTaskActivate, completeActivityState); err != nil {
				return fmt.Errorf("%s failed to activate gateway: %w", errors.Fn(), err)
			}
		} else {
			if err := c.completeActivity(ctx, completeActivityState); err != nil {
				return fmt.Errorf("complete activity for exclusive gateway: %w", err)
			}
		}
	case element.ServiceTask:
		stID, err := c.ns.GetServiceTaskRoutingKey(ctx, el.Execute)
		if err != nil {
			return fmt.Errorf("get service task routing key during activity start processor: %w", err)
		}
		if err := c.startJob(ctx, messages.WorkflowJobServiceTaskExecute+"."+stID, newState, el, traversal.Vars); err != nil {
			return c.engineErr(ctx, "start srvice task job", err, apErrFields(pi.ProcessInstanceId, pi.WorkflowId, el.Id, el.Name, el.Type, process.Name)...)
		}
	case element.UserTask:
		if err := c.startJob(ctx, messages.WorkflowJobUserTaskExecute, newState, el, traversal.Vars); err != nil {
			return c.engineErr(ctx, "start user task job", err, apErrFields(pi.ProcessInstanceId, pi.WorkflowId, el.Id, el.Name, el.Type, process.Name)...)
		}
	case element.ManualTask:
		if err := c.startJob(ctx, messages.WorkflowJobManualTaskExecute, newState, el, traversal.Vars); err != nil {
			return c.engineErr(ctx, "start manual task job", err, apErrFields(pi.ProcessInstanceId, pi.WorkflowId, el.Id, el.Name, el.Type, process.Name)...)
		}
	case element.MessageIntermediateThrowEvent:
		wf, err := c.ns.GetWorkflow(ctx, pi.WorkflowId)
		if err != nil {
			return fmt.Errorf("get workflow for intermediate throw event: %w", err)
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
			return &errors.ErrWorkflowFatal{Err: fmt.Errorf("unknown workflow message name: %s", el.Execute)}
		}
		msgState := common.CopyWorkflowState(newState)
		msgState.Condition = &wf.Messages[ix].Execute
		if err := c.startJob(ctx, messages.WorkflowJobSendMessageExecute+"."+pi.WorkflowName+"_"+el.Execute, newState, el, traversal.Vars); err != nil {
			return c.engineErr(ctx, "start message job", err, apErrFields(pi.ProcessInstanceId, pi.WorkflowId, el.Id, el.Name, el.Type, process.Name)...)
		}
	case element.CallActivity:
		if err := c.startJob(ctx, subj.NS(messages.WorkflowJobLaunchExecute, "default"), newState, el, traversal.Vars); err != nil {
			return c.engineErr(ctx, "start message lauch", err, apErrFields(pi.ProcessInstanceId, pi.WorkflowId, el.Id, el.Name, el.Type, process.Name)...)
		}
	case element.MessageIntermediateCatchEvent:
		awaitMsg := common.CopyWorkflowState(newState)
		awaitMsg.Execute = &el.Execute
		awaitMsg.Condition = &el.Msg
		if err := c.startJob(ctx, messages.WorkflowJobAwaitMessageExecute, awaitMsg, el, awaitMsg.Vars); err != nil {
			return c.engineErr(ctx, "start await message task job", err, apErrFields(pi.ProcessInstanceId, pi.WorkflowId, el.Id, el.Name, el.Type, process.Name)...)
		}
	case element.TimerIntermediateCatchEvent:
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
		newState.Id = common.TrackingID(newState.Id).Push(ksuid.New().String())
		if err := c.ns.PublishWorkflowState(ctx, subj.NS(messages.WorkflowJobTimerTaskExecute, "default"), newState); err != nil {
			return fmt.Errorf("publish timer task execute job: %w", err)
		}
		if err := c.ns.PublishWorkflowState(ctx, subj.NS(messages.WorkflowJobTimerTaskComplete, "default"), newState, storage.WithEmbargo(embargo)); err != nil {
			return fmt.Errorf("publish timer task execute complete: %w", err)
		}
	case element.EndEvent:
		if pi.ParentProcessId == nil || *pi.ParentProcessId == "" {
			if len(el.Errors) == 0 {
				status = model.CancellationState_completed
			} else {
				status = model.CancellationState_errored
			}
		}
		newState.State = status
		if err := c.completeActivity(ctx, newState); err != nil {
			return fmt.Errorf("complete activity for end event: %w", err)
		}
	case element.LinkIntermediateThrowEvent:
		newState.State = status
		if err := c.completeActivity(ctx, newState); err != nil {
			return fmt.Errorf("default complete activity: %w", err)
		}
	default:
		// if we don't support the event, just traverse to the next element
		newState.State = status
		if err := c.completeActivity(ctx, newState); err != nil {
			return fmt.Errorf("default complete activity: %w", err)
		}
	}

	// if the workflow is complete, send an instance complete message to trigger tidy up
	if status == model.CancellationState_completed || status == model.CancellationState_errored || status == model.CancellationState_terminated {
		newState.Id = []string{pi.ProcessInstanceId}
		newState.State = status
		newState.Error = el.Error
		if err := c.ns.PublishWorkflowState(ctx, messages.WorkflowProcessComplete, newState); err != nil {
			return c.engineErr(ctx, "publish workflow status", err, apErrFields(pi.ProcessInstanceId, pi.WorkflowId, el.Id, el.Name, el.Type, process.Name)...)
		}
	}
	return nil
}

func (c *Engine) completeActivity(ctx context.Context, state *model.WorkflowState) error {
	// tell the world that we processed the activity
	common.DropStateParams(state)
	if err := c.ns.PublishWorkflowState(ctx, messages.WorkflowActivityComplete, state); err != nil {
		return c.engineErr(ctx, "publish workflow cancellationState", err)
		//TODO: report this without process: apErrFields(wfi.WorkflowInstanceId, wfi.WorkflowId, el.Id, el.Name, el.Type, process.Name)
	}
	if err := c.ns.RecordHistoryActivityComplete(ctx, state); err != nil {
		return c.engineErr(ctx, "record history activity complete", &errors.ErrWorkflowFatal{Err: err})
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
	if _, err := c.ns.GetOldState(ctx, common.TrackingID(job.Id).ParentID()); errors2.Is(err, errors.ErrStateNotFound) {
		// We can't find the job's saved state
		return nil
	} else if err != nil {
		return fmt.Errorf("get old state for complete job processor: %w", err)
	}

	// TODO: CHeck Workflow instance exists
	// get the relevant workflow instance
	pi, err := c.ns.GetProcessInstance(ctx, job.ProcessInstanceId)
	if errors2.Is(err, errors.ErrProcessInstanceNotFound) {
		// if the instance has been deleted quash this activity
		log.Warn("process instance not found, cancelling job processing", err, slog.String(keys.WorkflowInstanceID, job.WorkflowInstanceId))
		return nil
	} else if err != nil {
		log.Warn("get process instance for job", err,
			slog.String(keys.JobType, job.ElementType),
			slog.String(keys.JobID, common.TrackingID(job.Id).ID()),
		)
		return fmt.Errorf("get workflow instance for job: %w", err)
	}

	// get the relevant workflow
	wf, err := c.ns.GetWorkflow(ctx, pi.WorkflowId)
	if err != nil {
		return c.engineErr(ctx, "fetch job workflow", err,
			slog.String(keys.JobType, job.ElementType),
			slog.String(keys.JobID, common.TrackingID(job.Id).ID()),
			slog.String(keys.WorkflowInstanceID, pi.WorkflowInstanceId),
			slog.String(keys.WorkflowID, pi.WorkflowId),
		)
	}
	// build element table
	els := common.ElementTable(wf)
	el := els[job.ElementId]
	newID := common.TrackingID(job.Id).Pop()
	oldState, err := c.ns.GetOldState(ctx, newID.ID())
	if errors2.Is(err, errors.ErrStateNotFound) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("complete job processor failed to get old state: %w", err)
	}
	if err := vars.OutputVars(ctx, job.Vars, &oldState.Vars, el.OutputTransform); err != nil {
		return fmt.Errorf("complete job processor failed to transform variables: %w", err)
	}
	completeActivityState := common.CopyWorkflowState(oldState)
	completeActivityState.Id = newID
	completeActivityState.State = job.State
	completeActivityState.ElementId = el.Id
	completeActivityState.ElementType = el.Type
	if err := c.completeActivity(ctx, completeActivityState); err != nil {
		return fmt.Errorf("complete job processor failed to complete activity: %w", err)
	}
	if err := c.ns.DeleteJob(ctx, common.TrackingID(job.Id).ID()); err != nil {
		return fmt.Errorf("complete job processor failed to delete job: %w", err)
	}
	return nil
}

// startJob launches a user/service task
func (c *Engine) startJob(ctx context.Context, subject string, job *model.WorkflowState, el *model.Element, v []byte, opts ...storage.PublishOpt) error {
	job.Execute = &el.Execute

	// skip if this job type requires no input transformation
	if el.Type != element.MessageIntermediateCatchEvent {
		job.Vars = nil
		if err := vars.InputVars(ctx, v, &job.Vars, el); err != nil {
			return errors.ErrWorkflowFatal{Err: fmt.Errorf("start job failed to get input variables: %w", err)}
		}
	}
	// if this is a user task, find out who can perfoem it
	if el.Type == element.UserTask {
		vx, err := vars.Decode(ctx, v)
		if err != nil {
			return errors.ErrWorkflowFatal{Err: fmt.Errorf("start job failed to decode input variables: %w", err)}
		}

		owners, err := c.evaluateOwners(ctx, el.Candidates, vx)
		if err != nil {
			return errors.ErrWorkflowFatal{Err: fmt.Errorf("start job failed to evaluate owners: %w", err)}
		}
		groups, err := c.evaluateOwners(ctx, el.CandidateGroups, vx)
		if err != nil {
			return errors.ErrWorkflowFatal{Err: fmt.Errorf("start job failed to evaluate groups: %w", err)}
		}

		job.Owners = owners
		job.Groups = groups
	}

	// create the job
	_, err := c.ns.CreateJob(ctx, job)
	if err != nil {
		return c.engineErr(ctx, "start manual task", err,
			slog.String(keys.ElementName, el.Name),
			slog.String(keys.ElementID, el.Id),
			slog.String(keys.ElementType, el.Type),
			slog.String(keys.JobType, job.ElementType),
			slog.String(keys.JobID, common.TrackingID(job.Id).ID()),
			slog.String(keys.WorkflowInstanceID, job.WorkflowInstanceId),
		)
	}
	/*
		//Save Iterator State
		common.SaveLargeObj

		// Multi-instance
		if el.Iteration != nil {
			if el.Iteration.Execute == model.ThreadingType_Sequential {
				// Launch as usual, just with iteration parameters
				seqVars, err := vars.Decode(ctx, job.Vars)
				if err != nil {
					return errors.ErrWorkflowFatal{Err: fmt.Errorf("start job failed to decode input variables: %w", err)}
				}
				collection, ok := seqVars[el.Iteration.Collection]
				if !ok {
					return errors.ErrWorkflowFatal{Err: fmt.Errorf("start job failed to decode input variables: %w", err)}
				}
				seqVars[el.Iteration.Iterator] = getCollectionIndex[collection]
			} else if model.ThreadingType_Parallel {

			}
		}
	*/
	// Single instance launch
	if el.Iteration == nil {

		if err := c.ns.PublishWorkflowState(ctx, subj.NS(subject, "default"), job, opts...); err != nil {
			return fmt.Errorf("start job failed to publish: %w", err)
		}
		// finally tell the engine that the job is ready for a client
		return nil
	}
	return nil
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
			return nil, fmt.Errorf("evaluate owners failed to get owner ID: %w", err)
		}
		jobGroups[i] = id
	}
	return jobGroups, nil
}

func (c *Engine) engineErr(ctx context.Context, msg string, err error, z ...any) error {
	log := logx.FromContext(ctx)
	z = append(z, "error", err.Error())
	log.Error(msg, err, z)

	return fmt.Errorf("engine-error: %w", err)
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
func (c *Engine) CancelWorkflowInstance(ctx context.Context, state *model.WorkflowState) error {
	if state.State == model.CancellationState_executing {
		return fmt.Errorf("executing is an invalid cancellation state: %w", errors.ErrInvalidState)
	}
	if err := c.ns.XDestroyWorkflowInstance(ctx, state); err != nil {
		return fmt.Errorf("cancel workflow instance failed: %w", errors.ErrCancelFailed)
	}
	return nil
}

// CompleteManualTask completes a manual workflow task
func (c *Engine) CompleteManualTask(ctx context.Context, job *model.WorkflowState, newvars []byte) error {
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
		return fmt.Errorf("complete manual task failed to publish manual task complete message: %w", err)
	}
	return nil
}

// CompleteServiceTask completes a workflow service task
func (c *Engine) CompleteServiceTask(ctx context.Context, job *model.WorkflowState, newvars []byte) error {
	if _, err := c.ns.GetOldState(ctx, common.TrackingID(job.Id).ParentID()); errors2.Is(err, errors.ErrStateNotFound) {
		if err := c.ns.PublishWorkflowState(ctx, subj.NS(messages.WorkflowJobServiceTaskAbort, "default"), job); err != nil {
			return fmt.Errorf("complete service task failed to publish workflow state: %w", err)
		}
		return nil
	} else if err != nil {
		return fmt.Errorf("complete service task failed to get old state: %w", err)
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
		return fmt.Errorf("complete service task failed to publish service task complete message: %w", err)
	}
	return nil
}

// CompleteSendMessageTask completes a send message task
func (c *Engine) CompleteSendMessageTask(ctx context.Context, job *model.WorkflowState, newvars []byte) error {
	_, err := c.ns.GetElement(ctx, job)
	if err != nil {
		return &errors.ErrWorkflowFatal{Err: err}
	}
	if err := c.ns.PublishWorkflowState(ctx, messages.WorkflowJobSendMessageComplete, job); err != nil {
		return fmt.Errorf("complete send message task failed to publish send message complete nats message: %w", err)
	}
	return nil
}

// CompleteUserTask completes and closes a user task with variables
func (c *Engine) CompleteUserTask(ctx context.Context, job *model.WorkflowState, newvars []byte) error {
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
		return fmt.Errorf("complete user task failed to publish user task complete message: %w", err)
	}
	if err := c.ns.CloseUserTask(ctx, common.TrackingID(job.Id).ID()); err != nil {
		return fmt.Errorf("complete user task failed to close user task: %w", err)
	}
	return nil
}

func (c *Engine) activityCompleteProcessor(ctx context.Context, state *model.WorkflowState) error {
	ctx, log := logx.ContextWith(ctx, "engine.activityCompleteProcessor")
	if old, err := c.ns.GetOldState(ctx, common.TrackingID(state.Id).ID()); errors2.Is(err, errors.ErrStateNotFound) {
		log.Warn("old state not found", slog.Any("error", err))
		return nil
	} else if err != nil {
		return fmt.Errorf("activity complete processor failed to get old state: %w", err)
	} else if old.State == model.CancellationState_obsolete && state.State == model.CancellationState_obsolete {
		return nil
	}

	pi, pierr := c.ns.GetProcessInstance(ctx, state.ProcessInstanceId)
	if errors2.Is(pierr, errors.ErrProcessInstanceNotFound) {
		errTxt := "process instance not found"
		log.Warn(errTxt, slog.String(keys.ProcessInstanceID, state.WorkflowInstanceId))
	} else if pierr != nil {
		return fmt.Errorf("activity complete processor failed to get process instance: %w", pierr)
	}

	wf, err := c.ns.GetWorkflow(ctx, state.WorkflowId)
	if err != nil {
		return fmt.Errorf("activity complete processor failed to get workflow: %w", err)
	}

	els := common.ElementTable(wf)
	newID := common.TrackingID(state.Id).Pop()
	el := els[state.ElementId]
	// intermediateLinkCatchEvent - manually attach outbound connections.
	if el.Type == element.LinkIntermediateThrowEvent {
		var target *model.Element
		for _, elTest := range els {
			if elTest.Type == element.LinkIntermediateCatchEvent && elTest.Execute == el.Execute {
				target = elTest
				break
			}
		}
		if target == nil {
			return &errors.ErrWorkflowFatal{Err: errors2.New("corresponding catch not found")}
		}
		el.Outbound = &model.Targets{Target: []*model.Target{{Target: target.Id}}}
	}
	if pierr == nil {
		if err = c.traverse(ctx, pi, newID, el.Outbound, els, state); errors.IsWorkflowFatal(err) {
			log.Error("workflow fatally terminated whilst traversing", err, slog.String(keys.ProcessInstanceID, pi.ProcessInstanceId), slog.String(keys.WorkflowID, pi.WorkflowId), slog.String(keys.ElementID, state.ElementId))
			return nil
		} else if err != nil {
			return fmt.Errorf("activity complete processor failed traversal attempt: %w", err)
		}
	}
	if state.ElementType == element.EndEvent && len(state.Id) > 2 {
		jobID := common.TrackingID(state.Id).Ancestor(2)
		// If we are a sub workflow then complete the parent job
		if jobID != state.WorkflowInstanceId {
			j, joberr := c.ns.GetJob(ctx, jobID)
			if errors2.Is(joberr, errors.ErrJobNotFound) {
				log.Warn("job not found " + jobID + " : " + err.Error())
			} else if joberr != nil {
				return fmt.Errorf("activity complete processor failed to get job: %w", joberr)
			}
			if joberr == nil {
				j.Vars = state.Vars
				j.Error = state.Error
				if err := c.ns.PublishWorkflowState(ctx, messages.WorkflowJobLaunchComplete, j); err != nil {
					return fmt.Errorf("activity complete processor failed to publish job launch complete: %w", err)
				}
			}
			if err := c.ns.DeleteJob(ctx, jobID); err != nil && !errors2.Is(err, nats.ErrKeyNotFound) {
				return fmt.Errorf("activity complete processor failed to delete job %s: %w", jobID, err)
			}
			wi, wierr := c.ns.GetWorkflowInstance(ctx, state.WorkflowInstanceId)
			if wierr != nil && !errors2.Is(wierr, nats.ErrKeyNotFound) {
				return fmt.Errorf("activity complete processor failed to get workflow instance: %w", err)
			}
			if pierr == nil {
				if err := c.ns.DestroyProcessInstance(ctx, state, pi, wi); err != nil && !errors2.Is(err, nats.ErrKeyNotFound) {
					return fmt.Errorf("activity complete processor failed to destroy workflow instance: %w", err)
				}
			}
		}
	}
	return nil
}

func (c *Engine) launchProcessor(ctx context.Context, state *model.WorkflowState) error {
	wf, err := c.ns.GetWorkflow(ctx, state.WorkflowId)
	if err != nil {
		return &errors.ErrWorkflowFatal{Err: errors.ErrWorkflowNotFound}
	}
	els := common.ElementTable(wf)
	if _, _, err := c.launch(ctx, els[state.ElementId].Execute, state.Id, state.Vars, state.WorkflowInstanceId, state.ElementId); err != nil {
		return c.engineErr(ctx, "launch child workflow", &errors.ErrWorkflowFatal{Err: err})
	}
	return nil
}

func (c *Engine) timedExecuteProcessor(ctx context.Context, state *model.WorkflowState, wi *model.WorkflowInstance, due int64) (bool, int, error) {
	ctx, log := logx.ContextWith(ctx, "engine.timedExecuteProcessor")
	slog.Info("timedExecuteProcessor")
	wf, err := c.ns.GetWorkflow(ctx, state.WorkflowId)
	if err != nil {
		log.Error("get timer proto workflow: %s", err)
		return true, 0, fmt.Errorf("get timer proto workflow: %w", err)
	}

	els := common.ElementTable(wf)
	el := els[state.ElementId]

	now := time.Now().UnixNano()
	lastFired := state.Timer.LastFired
	elapsed := now - lastFired
	count := state.Timer.Count
	repeat := el.Timer.Repeat
	value := el.Timer.Value
	fireNext := lastFired + (value * 2)

	newTimer := proto.Clone(state).(*model.WorkflowState)

	newTimer.Timer = &model.WorkflowTimer{
		LastFired: now,
		Count:     count + 1,
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
			if err := c.ns.SatisfyProcess(ctx, wi, state.ProcessName); err != nil {
				return false, 0, fmt.Errorf("timedExecuteProcessor failed to satisfy a process upon time completion: %w", err)
			}
			return true, 0, nil
		}
		isTimer = true
		shouldFire = elapsed >= value
	}

	if isTimer {
		if shouldFire {
			pi, err := c.ns.CreateProcessInstance(ctx, state.WorkflowInstanceId, "", "", state.ProcessName)
			if err != nil {
				log.Error("creating timed process instance", err)
				return false, 0, fmt.Errorf("creating timed workflow instance: %w", err)
			}
			state.ProcessInstanceId = pi.ProcessInstanceId
			if err := c.ns.PublishWorkflowState(ctx, messages.WorkflowProcessExecute, state); err != nil {
				log.Error("spawning process", err)
				return false, 0, nil
			}
			if err := c.ns.RecordHistoryProcessStart(ctx, state); err != nil {
				log.Error("start events record process start", err)
				return false, 0, fmt.Errorf("publish initial traversal: %w", err)
			}
			if err := vars.OutputVars(ctx, newTimer.Vars, &newTimer.Vars, el.OutputTransform); err != nil {
				log.Error("merging variables", err)
				return false, 0, nil
			}
			if err := c.traverse(ctx, pi, []string{ksuid.New().String()}, el.Outbound, els, state); err != nil {
				log.Error("traversing for timed workflow instance", err)
				return false, 0, nil
			}
			if err := c.ns.PublishWorkflowState(ctx, messages.WorkflowTimedExecute, newTimer); err != nil {
				log.Error("publishing timer", err)
				return false, int(fireNext), nil
			}
		} else if el.Timer.Type == model.WorkflowTimerType_duration {
			fmt.Println("nak with delay")
			return false, int(fireNext - now), nil
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
