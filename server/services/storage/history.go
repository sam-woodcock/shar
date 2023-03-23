package storage

import (
	"context"
	"fmt"
	"gitlab.com/shar-workflow/shar/common"
	"gitlab.com/shar-workflow/shar/model"
	"gitlab.com/shar-workflow/shar/server/errors"
)

// RecordHistoryProcessStart records the process start into the history object.
func (s *Nats) RecordHistoryProcessStart(ctx context.Context, state *model.WorkflowState) error {
	e := &model.ProcessHistoryEntry{
		ItemType:           model.ProcessHistoryType_processExecute,
		WorkflowId:         &state.WorkflowId,
		WorkflowInstanceId: &state.WorkflowInstanceId,
		CancellationState:  &state.State,
		Vars:               state.Vars,
		Timer:              state.Timer,
		Error:              state.Error,
		UnixTimeNano:       state.UnixTimeNano,
		Execute:            state.Execute,
	}
	ph := &model.ProcessHistory{Item: []*model.ProcessHistoryEntry{e}}
	if err := common.SaveObj(ctx, s.wfHistory, state.ProcessInstanceId, ph); err != nil {
		return &errors.ErrWorkflowFatal{Err: fmt.Errorf("recording history for process start: %w", err)}
	}
	return nil
}

// RecordHistoryActivityExecute records the activity execute into the history object.
func (s *Nats) RecordHistoryActivityExecute(ctx context.Context, state *model.WorkflowState) error {
	e := &model.ProcessHistoryEntry{
		ItemType:          model.ProcessHistoryType_activityExecute,
		ElementId:         &state.ElementId,
		CancellationState: &state.State,
		Vars:              state.Vars,
		Timer:             state.Timer,
		Error:             state.Error,
		UnixTimeNano:      state.UnixTimeNano,
		Execute:           state.Execute,
	}
	ph := &model.ProcessHistory{}
	if err := common.UpdateObj(ctx, s.wfHistory, state.ProcessInstanceId, ph, func(v *model.ProcessHistory) (*model.ProcessHistory, error) {
		v.Item = append(v.Item, e)
		return v, nil
	}); err != nil {
		return &errors.ErrWorkflowFatal{Err: fmt.Errorf("recording history for activity execute: %w", err)}
	}
	return nil
}

// RecordHistoryActivityComplete records the activity completion into the history object.
func (s *Nats) RecordHistoryActivityComplete(ctx context.Context, state *model.WorkflowState) error {
	e := &model.ProcessHistoryEntry{
		ItemType:          model.ProcessHistoryType_activityComplete,
		ElementId:         &state.ElementId,
		CancellationState: &state.State,
		Vars:              state.Vars,
		Timer:             state.Timer,
		Error:             state.Error,
		UnixTimeNano:      state.UnixTimeNano,
	}
	ph := &model.ProcessHistory{}
	if err := common.UpdateObj(ctx, s.wfHistory, state.ProcessInstanceId, ph, func(v *model.ProcessHistory) (*model.ProcessHistory, error) {
		v.Item = append(v.Item, e)
		return v, nil
	}); err != nil {
		return &errors.ErrWorkflowFatal{Err: fmt.Errorf("recording history for ectivity complete: %w", err)}
	}
	return nil
}

// RecordHistoryProcessComplete records the process completion into the history object.
func (s *Nats) RecordHistoryProcessComplete(ctx context.Context, state *model.WorkflowState) error {
	e := &model.ProcessHistoryEntry{
		ItemType:          model.ProcessHistoryType_processComplete,
		CancellationState: &state.State,
		Vars:              state.Vars,
		Timer:             state.Timer,
		Error:             state.Error,
		UnixTimeNano:      state.UnixTimeNano,
	}
	ph := &model.ProcessHistory{}
	if err := common.UpdateObj(ctx, s.wfHistory, state.ProcessInstanceId, ph, func(v *model.ProcessHistory) (*model.ProcessHistory, error) {
		v.Item = append(v.Item, e)
		return v, nil
	}); err != nil {
		return &errors.ErrWorkflowFatal{Err: fmt.Errorf("recording history for process complete: %w", err)}
	}
	return nil
}

// RecordHistoryProcessSpawn records the process spawning a new process into the history object.
func (s *Nats) RecordHistoryProcessSpawn(ctx context.Context, state *model.WorkflowState, newProcessInstanceID string) error {
	e := &model.ProcessHistoryEntry{
		ItemType:          model.ProcessHistoryType_processSpawnSync,
		CancellationState: &state.State,
		Vars:              state.Vars,
		Timer:             state.Timer,
		Error:             state.Error,
		UnixTimeNano:      state.UnixTimeNano,
	}
	ph := &model.ProcessHistory{}
	if err := common.UpdateObj(ctx, s.wfHistory, state.ProcessInstanceId, ph, func(v *model.ProcessHistory) (*model.ProcessHistory, error) {
		v.Item = append(v.Item, e)
		return v, nil
	}); err != nil {
		return &errors.ErrWorkflowFatal{Err: fmt.Errorf("recording history for process start: %w", err)}
	}
	return nil
}

// RecordHistoryProcessAbort records the process aborting into the history object.
func (s *Nats) RecordHistoryProcessAbort(ctx context.Context, state *model.WorkflowState) error {
	e := &model.ProcessHistoryEntry{
		ItemType:          model.ProcessHistoryType_processAbort,
		CancellationState: &state.State,
		Vars:              state.Vars,
		Timer:             state.Timer,
		Error:             state.Error,
		UnixTimeNano:      state.UnixTimeNano,
	}
	ph := &model.ProcessHistory{}
	if err := common.UpdateObj(ctx, s.wfHistory, state.ProcessInstanceId, ph, func(v *model.ProcessHistory) (*model.ProcessHistory, error) {
		v.Item = append(v.Item, e)
		return v, nil
	}); err != nil {
		return &errors.ErrWorkflowFatal{Err: fmt.Errorf("recording history for process complete: %w", err)}
	}
	return nil
}

// GetProcessHistory fetches the history object for a process.
func (s *Nats) GetProcessHistory(ctx context.Context, processInstanceId string) ([]*model.ProcessHistoryEntry, error) {
	ph := &model.ProcessHistory{}
	if err := common.LoadObj(ctx, s.wfHistory, processInstanceId, ph); err != nil {
		return nil, fmt.Errorf("fetching history for process: %w", err)
	}
	return ph.Item, nil
}
