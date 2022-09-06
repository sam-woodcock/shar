package workflow

//go:generate mockery --name NatsService --outpkg workflow --filename service_mock_test.go --output . --structname MockNatsService

import (
	"context"
	"gitlab.com/shar-workflow/shar/common"
	"gitlab.com/shar-workflow/shar/model"
	"gitlab.com/shar-workflow/shar/server/services"
)

type NatsService interface {
	AwaitMsg(ctx context.Context, state *model.WorkflowState) error
	ListWorkflows(ctx context.Context) (chan *model.ListWorkflowResult, chan error)
	StoreWorkflow(ctx context.Context, wf *model.Workflow) (string, error)
	GetWorkflow(ctx context.Context, workflowId string) (*model.Workflow, error)
	CreateWorkflowInstance(ctx context.Context, wfInstance *model.WorkflowInstance) (*model.WorkflowInstance, error)
	GetWorkflowInstance(ctx context.Context, workflowInstanceId string) (*model.WorkflowInstance, error)
	DestroyWorkflowInstance(ctx context.Context, workflowInstanceId string, state model.CancellationState, wfError *model.Error) error
	GetServiceTaskRoutingKey(taskName string) (string, error)
	GetMessageSenderRoutingKey(workflowName string, messageName string) (string, error)
	GetLatestVersion(ctx context.Context, workflowName string) (string, error)
	CreateJob(ctx context.Context, job *model.WorkflowState) (string, error)
	GetJob(ctx context.Context, id string) (*model.WorkflowState, error)
	ListWorkflowInstance(ctx context.Context, workflowName string) (chan *model.ListWorkflowInstanceResult, chan error)
	GetWorkflowInstanceStatus(ctx context.Context, id string) (*model.WorkflowInstanceStatus, error)
	StartProcessing(ctx context.Context) error
	SetEventProcessor(processor services.EventProcessorFunc)
	SetMessageCompleteProcessor(processor services.MessageCompleteProcessorFunc)
	SetCompleteJobProcessor(processor services.CompleteJobProcessorFunc)
	PublishWorkflowState(ctx context.Context, stateName string, state *model.WorkflowState, delay int) error
	PublishMessage(ctx context.Context, workflowInstanceID string, name string, key string, vars []byte) error
	Conn() common.NatsConn
	Shutdown()
	CloseUserTask(ctx context.Context, trackingID string) error
	OwnerId(name string) (string, error)
	OwnerName(id string) (string, error)
}
