package workflow

import (
	"context"
	"github.com/segmentio/ksuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"gitlab.com/shar-workflow/shar/common"
	"gitlab.com/shar-workflow/shar/model"
	"gitlab.com/shar-workflow/shar/server/vars"
	"testing"
)

func TestLaunchWorkflow(t *testing.T) {
	ctx := context.Background()

	_, eng, svc, wf := setupTestWorkflow(t, "simple-workflow.bpmn")

	process := wf.Process["WorkflowDemo"]
	els := make(map[string]*model.Element)
	common.IndexProcessElements(process.Elements, els)

	svc.On("GetLatestVersion", mock.AnythingOfType("*context.emptyCtx"), mock.AnythingOfType("string")).
		Once().
		Return("test-workflow-id", nil)

	svc.On("GetWorkflow", mock.AnythingOfType("*context.emptyCtx"), "test-workflow-id").
		Once().
		Return(wf, nil)

	svc.On("CreateWorkflowInstance", mock.AnythingOfType("*context.emptyCtx"), mock.AnythingOfType("*model.WorkflowInstance")).
		Once().
		Return(&model.WorkflowInstance{
			WorkflowInstanceId:       "test-workflow-instance-id",
			ParentWorkflowInstanceId: nil,
			ParentElementId:          nil,
			WorkflowId:               "test-workflow-id",
		}, nil)

	svc.On("PublishWorkflowState", mock.AnythingOfType("*context.emptyCtx"), "WORKFLOW.%s.State.Traversal.Execute", mock.AnythingOfType("*model.WorkflowState")).
		Once().
		Run(func(args mock.Arguments) {
			assert.Equal(t, args[2].(*model.WorkflowState).WorkflowId, "test-workflow-id")
			assert.Equal(t, args[2].(*model.WorkflowState).WorkflowInstanceId, "test-workflow-instance-id")
			assert.Equal(t, args[2].(*model.WorkflowState).ElementId, "StartEvent")
			assert.Equal(t, args[2].(*model.WorkflowState).ElementType, "startEvent")
		}).
		Return(nil)

	svc.On("PublishWorkflowState", mock.AnythingOfType("*context.emptyCtx"), "WORKFLOW.%s.State.Workflow.Execute", mock.AnythingOfType("*model.WorkflowState")).
		Once().
		Run(func(args mock.Arguments) {
			assert.Equal(t, args[2].(*model.WorkflowState).WorkflowId, "test-workflow-id")
			assert.Equal(t, args[2].(*model.WorkflowState).WorkflowInstanceId, "test-workflow-instance-id")
		}).
		Return(nil)

	wfiid, err := eng.Launch(ctx, "TestWorkflow", []byte{})
	assert.NoError(t, err)
	assert.Equal(t, "test-workflow-instance-id", wfiid)
	svc.AssertExpectations(t)

}

func TestTraversal(t *testing.T) {
	ctx := context.Background()

	_, eng, svc, wf := setupTestWorkflow(t, "simple-workflow.bpmn")

	process := wf.Process["WorkflowDemo"]
	els := make(map[string]*model.Element)
	common.IndexProcessElements(process.Elements, els)

	wfi := &model.WorkflowInstance{
		WorkflowInstanceId:       "test-workflow-instance-id",
		ParentWorkflowInstanceId: nil,
		ParentElementId:          nil,
		WorkflowId:               "test-workflow-id",
	}

	svc.On("PublishWorkflowState", mock.AnythingOfType("*context.emptyCtx"), "WORKFLOW.%s.State.Traversal.Execute", mock.AnythingOfType("*model.WorkflowState")).
		Once().
		Run(func(args mock.Arguments) {
			assert.Equal(t, args[2].(*model.WorkflowState).WorkflowId, "test-workflow-id")
			assert.Equal(t, args[2].(*model.WorkflowState).WorkflowInstanceId, "test-workflow-instance-id")
			assert.Equal(t, args[2].(*model.WorkflowState).ElementId, "Step1")
			assert.Equal(t, args[2].(*model.WorkflowState).ElementType, "serviceTask")
			assert.NotEmpty(t, args[2].(*model.WorkflowState).Id)
		}).
		Return(nil)

	err := eng.traverse(ctx, wfi, []string{ksuid.New().String()}, els["StartEvent"].Outbound, els, []byte{})
	assert.NoError(t, err)
	svc.AssertExpectations(t)

}

func TestActivityProcessorServiceTask(t *testing.T) {
	ctx := context.Background()

	_, eng, svc, wf := setupTestWorkflow(t, "simple-workflow.bpmn")

	process := wf.Process["WorkflowDemo"]
	els := make(map[string]*model.Element)
	common.IndexProcessElements(process.Elements, els)
	id := "ljksdadlksajkldkjsakl"
	svc.On("GetServiceTaskRoutingKey", mock.AnythingOfType("string")).Return(id, nil)

	svc.On("GetWorkflowInstance", mock.AnythingOfType("*context.emptyCtx"), "test-workflow-instance-id").
		Once().
		Return(&model.WorkflowInstance{
			WorkflowInstanceId:       "test-workflow-instance-id",
			ParentWorkflowInstanceId: nil,
			ParentElementId:          nil,
			WorkflowId:               "test-workflow-id",
		}, nil)

	svc.On("GetWorkflow", mock.AnythingOfType("*context.emptyCtx"), "test-workflow-id").
		Once().
		Return(wf, nil)

	svc.On("PublishWorkflowState", mock.AnythingOfType("*context.emptyCtx"), "WORKFLOW.%s.State.Activity.Execute", mock.AnythingOfType("*model.WorkflowState")).
		Once().
		Run(func(args mock.Arguments) {
			assert.Equal(t, args[2].(*model.WorkflowState).ElementId, "Step1")
			assert.Equal(t, args[2].(*model.WorkflowState).ElementType, "serviceTask")
		}).
		Return(nil)

	svc.On("PublishWorkflowState", mock.AnythingOfType("*context.emptyCtx"), "WORKFLOW.%s.State.Traversal.Complete", mock.AnythingOfType("*model.WorkflowState")).
		Once().
		Run(func(args mock.Arguments) {
			assert.Equal(t, args[2].(*model.WorkflowState).WorkflowId, "test-workflow-id")
			assert.Equal(t, args[2].(*model.WorkflowState).WorkflowInstanceId, "test-workflow-instance-id")
			assert.Equal(t, args[2].(*model.WorkflowState).ElementId, "Step1")
			assert.Equal(t, args[2].(*model.WorkflowState).ElementType, "serviceTask")
			assert.NotEmpty(t, args[2].(*model.WorkflowState).Id)
		}).
		Return(nil)

	svc.On("CreateJob", mock.AnythingOfType("*context.emptyCtx"), mock.AnythingOfType("*model.WorkflowState")).
		Once().
		Run(func(args mock.Arguments) {
			assert.Equal(t, args[1].(*model.WorkflowState).WorkflowId, "test-workflow-id")
			assert.Equal(t, args[1].(*model.WorkflowState).WorkflowInstanceId, "test-workflow-instance-id")
			assert.Equal(t, args[1].(*model.WorkflowState).ElementId, "Step1")
			assert.Equal(t, args[1].(*model.WorkflowState).ElementType, "serviceTask")
		}).
		Return("test-job-id", nil)

	svc.On("PublishWorkflowState", mock.AnythingOfType("*context.emptyCtx"), "WORKFLOW.default.State.Job.Execute.ServiceTask."+id, mock.AnythingOfType("*model.WorkflowState")).
		Once().
		Run(func(args mock.Arguments) {
			assert.Equal(t, args[2].(*model.WorkflowState).WorkflowId, "test-workflow-id")
			assert.Equal(t, args[2].(*model.WorkflowState).WorkflowInstanceId, "test-workflow-instance-id")
			assert.Equal(t, args[2].(*model.WorkflowState).ElementId, "Step1")
			assert.Equal(t, args[2].(*model.WorkflowState).ElementType, "serviceTask")
			//assert.NotEmpty(t, args[2].(*model.WorkflowState).TrackingId)
		}).
		Return(nil)
	trackingID := ksuid.New().String()
	v, err := vars.Encode(nil, model.Vars{})
	require.NoError(t, err)
	err = eng.activityStartProcessor(ctx, "", &model.WorkflowState{
		WorkflowInstanceId: "test-workflow-instance-id",
		ElementId:          els["Step1"].Id,
		Id:                 []string{trackingID},
		Vars:               v,
	}, false)
	assert.NoError(t, err)
	svc.AssertExpectations(t)

}

//TODO: RE-instate this test
/*
func TestCompleteJobProcessor(t *testing.T) {
	ctx := context.Background()

	_, eng, svc, wf := setupTestWorkflow(t, "simple-workflow.bpmn")

	process := wf.Process["WorkflowDemo"]
	els := make(map[string]*model.Element)
	common.IndexProcessElements(process.Elements, els)

	trackingID := ksuid.New().String()
	svc.On("GetJob", mock.AnythingOfType("*context.emptyCtx"), "test-job-id").
		Once().
		Return(&model.WorkflowState{
			WorkflowId:         "test-workflow-id",
			WorkflowInstanceId: "test-workflow-instance-id",
			ElementId:          "Step1",
			ElementType:        "serviceTask",
			Id:                 []string{trackingID},
			Execute:            nil,
			State:              model.CancellationState_Executing,
			Condition:          nil,
			UnixTimeNano:       0,
			Vars:               []byte{},
		}, nil)

	svc.On("GetWorkflowInstance", mock.AnythingOfType("*context.emptyCtx"), "test-workflow-instance-id").
		Once().
		Return(&model.WorkflowInstance{
			WorkflowInstanceId:       "test-workflow-instance-id",
			ParentWorkflowInstanceId: nil,
			ParentElementId:          nil,
			WorkflowId:               "test-workflow-id",
		}, nil)

	svc.On("GetWorkflow", mock.AnythingOfType("*context.emptyCtx"), "test-workflow-id").
		Once().
		Return(wf, nil)

	svc.On("PublishWorkflowState", mock.AnythingOfType("*context.emptyCtx"), "WORKFLOW.%s.State.Traversal.Execute", mock.AnythingOfType("*model.WorkflowState"), 0).
		Once().
		Run(func(args mock.Arguments) {
			assert.Equal(t, "test-workflow-id", args[2].(*model.WorkflowState).WorkflowId)
			assert.Equal(t, "test-workflow-instance-id", args[2].(*model.WorkflowState).WorkflowInstanceId)
			assert.Equal(t, "EndEvent", args[2].(*model.WorkflowState).ElementId)
			assert.Equal(t, "endEvent", args[2].(*model.WorkflowState).ElementType)
			assert.NotEmpty(t, args[2].(*model.WorkflowState).Id)
		}).
		Return(nil)

	err := eng.completeJobProcessor(ctx, []string{"test-job-id"}, []byte{})
	assert.NoError(t, err)
	svc.AssertExpectations(t)

}
*/
