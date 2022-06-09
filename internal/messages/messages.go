package messages

const (
	WorkflowJobExecuteAll          = "WORKFLOW.State.Job.Execute.*"
	WorkFlowJobCompleteAll         = "WORKFLOW.State.Job.Complete.*"
	WorkflowJobServiceTaskExecute  = "WORKFLOW.State.Job.Execute.ServiceTask"
	WorkflowJobServiceTaskComplete = "WORKFLOW.State.Job.Complete.ServiceTask"
	WorkflowJobUserTaskExecute     = "WORKFLOW.State.Job.Execute.UserTask"
	WorkflowJobUserTaskComplete    = "WORKFLOW.State.Job.Complete.UserTask"
	WorkflowJobManualTaskExecute   = "WORKFLOW.State.Job.Execute.ManualTask"
	WorkflowJobManualTaskComplete  = "WORKFLOW.State.Job.Complete.ManualTask"
	WorkflowInstanceAll            = "WORKFLOW.State.Workflow.*"
	WorkflowInstanceExecute        = "WORKFLOW.State.Workflow.Execute"
	WorkflowInstanceComplete       = "WORKFLOW.State.Workflow.Complete"
	WorkflowActivityAll            = "WORKFLOW.State.Activity.*"
	WorkflowActivityExecute        = "WORKFLOW.State.Activity.Execute"
	WorkflowActivityComplete       = "WORKFLOW.State.Activity.Complete"
	WorkflowTraversalExecute       = "WORKFLOW.State.Traversal.Execute"
	WorkflowTraversalComplete      = "WORKFLOW.State.Traversal.Complete"
)

const (
	ApiAll                    = "Workflow.Api.*"
	ApiStoreWorkflow          = "WORKFLOW.Api.StoreWorkflow"
	ApiLaunchWorkflow         = "WORKFLOW.Api.LaunchWorkflow"
	ApiListWorkflows          = "WORKFLOW.Api.ListWorkflows"
	ApiListWorkflowInstance   = "WORKFLOW.Api.ListWorkflowInstance"
	ApiGetWorkflowStatus      = "WORKFLOW.Api.GetWorkflowInstanceStatus"
	ApiCancelWorkflowInstance = "WORKFLOW.Api.CancelWorkflowInstance"
)

var AllMessages = []string{
	WorkflowJobServiceTaskExecute,
	WorkflowJobServiceTaskComplete,
	WorkflowJobUserTaskExecute,
	WorkflowJobUserTaskComplete,
	WorkflowJobManualTaskExecute,
	WorkflowJobManualTaskComplete,
	WorkflowInstanceExecute,
	WorkflowInstanceComplete,
	WorkflowActivityExecute,
	WorkflowActivityComplete,
	WorkflowTraversalExecute,
	WorkflowTraversalComplete,
	ApiAll,
}
