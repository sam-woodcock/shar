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
	WorkflowInstanceStart          = "WORKFLOW.Workflow.Start"
	WorkflowInstanceComplete       = "WORKFLOW.Workflow.Complete"
	WorkflowActivityAll            = "WORKFLOW.Activity.*"
	WorkflowActivityExecute        = "WORKFLOW.Activity.Execute"
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
	WorkflowInstanceStart,
	WorkflowInstanceComplete,
	WorkflowActivityExecute,
	WorkflowTraversalExecute,
	WorkflowTraversalComplete,
}
