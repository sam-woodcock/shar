package messages

const (
	WorkflowStateAll               = "WORKFLOW.State.>"
	WorkflowJobExecuteAll          = "WORKFLOW.State.Job.Execute.*"
	WorkFlowJobCompleteAll         = "WORKFLOW.State.Job.Complete.*"
	WorkflowJobServiceTaskExecute  = "WORKFLOW.State.Job.Execute.ServiceTask"
	WorkflowJobServiceTaskComplete = "WORKFLOW.State.Job.Complete.ServiceTask"
	WorkflowJobUserTaskExecute     = "WORKFLOW.State.Job.Execute.UserTask"
	WorkflowJobUserTaskComplete    = "WORKFLOW.State.Job.Complete.UserTask"
	WorkflowJobManualTaskExecute   = "WORKFLOW.State.Job.Execute.ManualTask"
	WorkflowJobManualTaskComplete  = "WORKFLOW.State.Job.Complete.ManualTask"
	WorkflowJobSendMessageExecute  = "WORKFLOW.State.Job.Execute.SendMessage"
	WorkflowJobSendMessageComplete = "WORKFLOW.State.Job.Complete.SendMessage"
	WorkflowInstanceExecute        = "WORKFLOW.State.Workflow.Execute"
	WorkflowInstanceComplete       = "WORKFLOW.State.Workflow.Complete"
	WorkflowInstanceTerminated     = "WORKFLOW.State.Workflow.Terminated"
	WorkflowInstanceAll            = "WORKFLOW.State.Workflow.>"
	WorkflowActivityExecute        = "WORKFLOW.State.Activity.Execute"
	WorkflowActivityComplete       = "WORKFLOW.State.Activity.Complete"
	WorkflowTraversalExecute       = "WORKFLOW.State.Traversal.Execute"
	WorkflowTraversalComplete      = "WORKFLOW.State.Traversal.Complete"
	WorkflowMessages               = "WORKFLOW.Message.>"
)

var WorkflowMessageFormat = "WORKFLOW.Message.%s.%s"

const (
	ApiAll                    = "Workflow.Api.*"
	ApiStoreWorkflow          = "WORKFLOW.Api.StoreWorkflow"
	ApiLaunchWorkflow         = "WORKFLOW.Api.LaunchWorkflow"
	ApiListWorkflows          = "WORKFLOW.Api.ListWorkflows"
	ApiListWorkflowInstance   = "WORKFLOW.Api.ListWorkflowInstance"
	ApiGetWorkflowStatus      = "WORKFLOW.Api.GetWorkflowInstanceStatus"
	ApiCancelWorkflowInstance = "WORKFLOW.Api.CancelWorkflowInstance"
	ApiSendMessage            = "WORKFLOW.Api.SendMessage"
	ApiCompleteManualTask     = "WORKFLOW.Api.CompleteManualTask"
	ApiCompleteServiceTask    = "WORKFLOW.Api.CompleteServiceTask"
	ApiCompleteUserTask       = "WORKFLOW.Api.CompleteUserTask"
	ApiListUserTaskIDs        = "WORKFLOW.Api.ListUserTaskIDs"
	ApiGetUserTask            = "WORKFLOW.Api.GetUserTask"
	ApiHandleWorkflowError    = "WORKFLOW.Api.HandleWorkflowError"
)

var AllMessages = []string{
	WorkflowJobServiceTaskExecute,
	WorkflowJobServiceTaskComplete,
	WorkflowJobUserTaskExecute,
	WorkflowJobUserTaskComplete,
	WorkflowJobManualTaskExecute,
	WorkflowJobManualTaskComplete,
	WorkflowJobSendMessageExecute,
	WorkflowJobSendMessageComplete,
	WorkflowInstanceExecute,
	WorkflowInstanceComplete,
	WorkflowInstanceTerminated,
	WorkflowActivityExecute,
	WorkflowActivityComplete,
	WorkflowTraversalExecute,
	WorkflowTraversalComplete,
	WorkflowMessages,
	ApiAll,
}

var (
	KvMessageSubs = "WORKFLOW_MSGSUBS"
	KvMessageSub  = "WORKFLOW_MSGSUB"
	KvJob         = "WORKFLOW_JOB"
	KvVersion     = "WORKFLOW_VERSION"
	KvDefinition  = "WORKFLOW_DEF"
	KvTracking    = "WORKFLOW_TRACKING"
	KvInstance    = "WORKFLOW_INSTANCE"
	KvMessageName = "WORKFLOW_MSGNAME"
	KvMessageID   = "WORKFLOW_MSGID"
	KvTrace       = "WORKFLOW_TRACKING"
	KvUserTask    = "WORKFLOW_USERTASK"
	KvOwnerName   = "WORKFLOW_OWNERNAME"
	KvOwnerId     = "WORKFLOW_OWNERID"
)
