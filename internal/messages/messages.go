package messages

const (
	WorkflowTraversal              = "WORKFLOW.Traversal"
	WorkflowJobExecuteAll          = "WORKFLOW.Job.Execute.*"
	WorkFlowJobCompleteAll         = "WORKFLOW.Job.Complete.*"
	WorkflowJobExecuteServiceTask  = "WORKFLOW.Job.Execute.ServiceTask"
	WorkflowJobCompleteServiceTask = "WORKFLOW.Job.Complete.ServiceTask"
	WorkflowJobExecuteUserTask     = "WORKFLOW.Job.Execute.UserTask"
	WorkflowJobCompleteUserTask    = "WORKFLOW.Job.Complete.UserTask"
	WorkflowJobExecuteManualTask   = "WORKFLOW.Job.Execute.ManualTask"
	WorkflowJobCompleteManualTask  = "WORKFLOW.Job.Complete.ManualTask"
	WorkflowInstanceAll            = "WORKFLOW.Workflow.*"
	WorkflowInstanceStart          = "WORKFLOW.Workflow.Start"
	WorkflowInstanceComplete       = "WORKFLOW.Workflow.Complete"
	WorkflowActivityAll            = "WORKFLOW.Activity.*"
	WorkflowActivityExecute        = "WORKFLOW.Activity.Execute"
	WorkflowAcivityTraverse        = "WORKFLOW.Activity.Traverse"
)
