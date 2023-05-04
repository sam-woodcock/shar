package workflow

//go:generate mockery --name NatsService --outpkg workflow --filename service_mock_test.go --output . --structname MockNatsService

import (
	"context"
	"gitlab.com/shar-workflow/shar/common"
	"gitlab.com/shar-workflow/shar/model"
	"gitlab.com/shar-workflow/shar/server/services"
	"gitlab.com/shar-workflow/shar/server/services/storage"
)

// NatsService is the shar type responsible for interacting with NATS.
type NatsService interface {
	SetTraversalProvider(provider services.TraversalFunc)
	ListWorkflows(ctx context.Context) (chan *model.ListWorkflowResult, chan error)
	StoreWorkflow(ctx context.Context, wf *model.Workflow) (string, error)
	GetWorkflow(ctx context.Context, workflowID string) (*model.Workflow, error)
	GetWorkflowVersions(ctx context.Context, workflowName string) (*model.WorkflowVersions, error)
	CreateWorkflowInstance(ctx context.Context, wfInstance *model.WorkflowInstance) (*model.WorkflowInstance, error)
	GetWorkflowInstance(ctx context.Context, workflowInstanceID string) (*model.WorkflowInstance, error)
	XDestroyWorkflowInstance(ctx context.Context, state *model.WorkflowState) error
	GetServiceTaskRoutingKey(ctx context.Context, taskName string, requiredId string) (string, error)
	GetLatestVersion(ctx context.Context, workflowName string) (string, error)
	CreateJob(ctx context.Context, job *model.WorkflowState) (string, error)
	GetJob(ctx context.Context, id string) (*model.WorkflowState, error)
	GetElement(ctx context.Context, state *model.WorkflowState) (*model.Element, error)
	ListWorkflowInstance(ctx context.Context, workflowName string) (chan *model.ListWorkflowInstanceResult, chan error)
	ListWorkflowInstanceProcesses(ctx context.Context, id string) ([]string, error)
	StartProcessing(ctx context.Context) error
	SetEventProcessor(processor services.EventProcessorFunc)
	SetMessageProcessor(processor services.MessageProcessorFunc)
	SetCompleteJobProcessor(processor services.CompleteJobProcessorFunc)
	SetCompleteActivity(processor services.CompleteActivityFunc)
	SetAbort(processor services.AbortFunc)
	DeleteJob(ctx context.Context, trackingID string) error
	SetCompleteActivityProcessor(processor services.CompleteActivityProcessorFunc)
	SetLaunchFunc(processor services.LaunchFunc)
	PublishWorkflowState(ctx context.Context, stateName string, state *model.WorkflowState, ops ...storage.PublishOpt) error
	PublishMessage(ctx context.Context, name string, key string, vars []byte) error
	Conn() common.NatsConn
	Shutdown()
	CloseUserTask(ctx context.Context, trackingID string) error
	OwnerID(name string) (string, error)
	OwnerName(id string) (string, error)
	GetOldState(ctx context.Context, id string) (*model.WorkflowState, error)
	CreateProcessInstance(ctx context.Context, workflowInstanceID string, parentProcessID string, parentElementID string, processName string) (*model.ProcessInstance, error)
	GetProcessInstance(ctx context.Context, processInstanceID string) (*model.ProcessInstance, error)
	DestroyProcessInstance(ctx context.Context, state *model.WorkflowState, pi *model.ProcessInstance, wi *model.WorkflowInstance) error
	SatisfyProcess(ctx context.Context, workflowInstance *model.WorkflowInstance, processName string) error
	GetGatewayInstanceID(state *model.WorkflowState) (string, string, error)
	GetGatewayInstance(ctx context.Context, gatewayInstanceID string) (*model.Gateway, error)
	RecordHistoryProcessStart(ctx context.Context, state *model.WorkflowState) error
	RecordHistoryActivityExecute(ctx context.Context, state *model.WorkflowState) error
	RecordHistoryProcessAbort(ctx context.Context, state *model.WorkflowState) error
	RecordHistoryActivityComplete(ctx context.Context, state *model.WorkflowState) error
	RecordHistoryProcessComplete(ctx context.Context, state *model.WorkflowState) error
	RecordHistoryProcessSpawn(ctx context.Context, state *model.WorkflowState, newProcessInstanceID string) error
	GetProcessHistory(ctx context.Context, processInstanceId string) ([]*model.ProcessHistoryEntry, error)
}
