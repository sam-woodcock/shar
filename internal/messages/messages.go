package messages

const (
	WorkflowJobExecuteAll          = "WORKFLOW.Job.Execute.*"
	WorkFlowJobCompleteAll         = "WORKFLOW.Job.Complete.*"
	WorkflowJobServiceTaskExecute  = "WORKFLOW.Job.Execute.ServiceTask"
	WorkflowJobServiceTaskComplete = "WORKFLOW.Job.Complete.ServiceTask"
	WorkflowJobUserTaskExecute     = "WORKFLOW.Job.Execute.UserTask"
	WorkflowJobUserTaskComplete    = "WORKFLOW.Job.Complete.UserTask"
	WorkflowJobManualTaskExecute   = "WORKFLOW.Job.Execute.ManualTask"
	WorkflowJobManualTaskComplete  = "WORKFLOW.Job.Complete.ManualTask"
	WorkflowInstanceAll            = "WORKFLOW.Workflow.*"
	WorkflowInstanceExecute        = "WORKFLOW.Workflow.Execute"
	WorkflowInstanceComplete       = "WORKFLOW.Workflow.Complete"
	WorkflowActivityAll            = "WORKFLOW.Activity.*"
	WorkflowActivityExecute        = "WORKFLOW.Activity.Execute"
	WorkflowActivityComplete       = "WORKFLOW.Activity.Complete"
	WorkflowTraversalExecute       = "WORKFLOW.Traversal.Execute"
	WorkflowTraversalComplete      = "WORKFLOW.Traversal.Complete"
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
}
