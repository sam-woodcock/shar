package services

import (
	"context"
	"github.com/crystal-construct/shar/model"
)

type Logging interface {
}

type Storage interface {
	// StoreWorkflow accepts a process, and returns a version sensitive workflowId
	StoreWorkflow(ctx context.Context, process *model.Process) (string, error)
	// GetWorkflow accepts a workflowId and returns the corresponding workflow version.
	GetWorkflow(ctx context.Context, workflowId string) (*model.Process, error)
	// GetWorkflowInstance gets a workflow based upon a workflowInstanceId
	GetWorkflowInstance(ctx context.Context, workflowInstanceId string) (*model.WorkflowInstance, error)
	// CreateWorkflowInstance accepts a workflowId and returns a workflowInstanceId
	CreateWorkflowInstance(ctx context.Context, instance *model.WorkflowInstance) (*model.WorkflowInstance, error)
	// DestroyWorkflowInstance destroys a workflow instance based upon a workflowInstanceId
	DestroyWorkflowInstance(ctx context.Context, workflowInstanceId string) error
	// GetLatestVersion returns the latest workflowId based upon a workflow name.
	GetLatestVersion(ctx context.Context, workflowName string) (string, error)
	CreateJob(ctx context.Context, job *model.WorkflowState) (string, error)
	GetJob(ctx context.Context, id string) (*model.WorkflowState, error)
	ListWorkflowInstance(workflowId string) (chan *model.ListWorkflowInstanceResult, chan error)
	ListWorkflows() (chan *model.ListWorkflowResult, chan error)
	GetWorkflowInstanceStatus(id string) (*model.WorkflowInstanceStatus, error)
}

type EventProcessorFunc func(ctx context.Context, workflowInstanceId, elementId, traversalId string, vars []byte) error
type CompleteJobProcessorFunc func(ctx context.Context, jobId string, vars []byte) error

type Queue interface {
	StartProcessing(ctx context.Context) error
	SetEventProcessor(processor EventProcessorFunc)
	// SetCompleteJobProcessor sets the engine receiver for job completions
	SetCompleteJobProcessor(processor CompleteJobProcessorFunc)
	// PublishWorkflowState publishes a workflow state message
	PublishWorkflowState(ctx context.Context, stateName string, message *model.WorkflowState) error
	PublishJob(ctx context.Context, stateName string, element *model.Element, message *model.WorkflowState) error
}
