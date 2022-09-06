package messages

import "gitlab.com/shar-workflow/shar/common/subj"

const (
	WorkflowStateAll                  = "WORKFLOW.%s.State.>"
	WorkflowJobExecuteAll             = "WORKFLOW.%s.State.Job.Execute.*"  //@
	WorkFlowJobCompleteAll            = "WORKFLOW.%s.State.Job.Complete.*" //@
	WorkflowJobServiceTaskExecute     = "WORKFLOW.%s.State.Job.Execute.ServiceTask"
	WorkflowJobServiceTaskExecuteWild = "WORKFLOW.%s.State.Job.Execute.ServiceTask.>"
	WorkflowJobServiceTaskComplete    = "WORKFLOW.%s.State.Job.Complete.ServiceTask"
	WorkflowJobUserTaskExecute        = "WORKFLOW.%s.State.Job.Execute.UserTask"
	WorkflowJobUserTaskComplete       = "WORKFLOW.%s.State.Job.Complete.UserTask"
	WorkflowJobManualTaskExecute      = "WORKFLOW.%s.State.Job.Execute.ManualTask"
	WorkflowJobManualTaskComplete     = "WORKFLOW.%s.State.Job.Complete.ManualTask"
	WorkflowJobSendMessageExecute     = "WORKFLOW.%s.State.Job.Execute.SendMessage"
	WorkflowJobSendMessageExecuteWild = "WORKFLOW.%s.State.Job.Execute.SendMessage.>"
	WorkflowJobSendMessageComplete    = "WORKFLOW.%s.State.Job.Complete.SendMessage"
	WorkflowInstanceExecute           = "WORKFLOW.%s.State.Workflow.Execute"
	WorkflowInstanceComplete          = "WORKFLOW.%s.State.Workflow.Complete"
	WorkflowInstanceTerminated        = "WORKFLOW.%s.State.Workflow.Terminated"
	WorkflowInstanceAll               = "WORKFLOW.%s.State.Workflow.>"
	WorkflowActivityExecute           = "WORKFLOW.%s.State.Activity.Execute"
	WorkflowActivityComplete          = "WORKFLOW.%s.State.Activity.Complete"
	WorkflowTraversalExecute          = "WORKFLOW.%s.State.Traversal.Execute"
	WorkflowTraversalComplete         = "WORKFLOW.%s.State.Traversal.Complete"
	WorkflowMessages                  = "WORKFLOW.%s.Message.>"
)

var AllMessages = []string{
	subj.SubjNS(WorkFlowJobCompleteAll, "default"),
	subj.SubjNS(WorkflowJobServiceTaskExecuteWild, "default"),
	subj.SubjNS(WorkflowJobSendMessageExecuteWild, "default"),
	subj.SubjNS(WorkflowJobServiceTaskComplete, "default"),
	subj.SubjNS(WorkflowJobUserTaskExecute, "default"),
	subj.SubjNS(WorkflowJobUserTaskComplete, "default"),
	subj.SubjNS(WorkflowJobManualTaskExecute, "default"),
	subj.SubjNS(WorkflowJobManualTaskComplete, "default"),
	subj.SubjNS(WorkflowJobSendMessageComplete, "default"),
	subj.SubjNS(WorkflowInstanceExecute, "default"),
	subj.SubjNS(WorkflowInstanceComplete, "default"),
	subj.SubjNS(WorkflowInstanceTerminated, "default"),
	subj.SubjNS(WorkflowActivityExecute, "default"),
	subj.SubjNS(WorkflowActivityComplete, "default"),
	subj.SubjNS(WorkflowTraversalExecute, "default"),
	subj.SubjNS(WorkflowTraversalComplete, "default"),
	subj.SubjNS(WorkflowMessages, "default"),
	ApiAll,
}

var WorkflowMessageFormat = "WORKFLOW.%s.Message.%s.%s"

const (
	ApiAll                       = "Workflow.Api.*"
	ApiStoreWorkflow             = "WORKFLOW.Api.StoreWorkflow"
	ApiLaunchWorkflow            = "WORKFLOW.Api.LaunchWorkflow"
	ApiListWorkflows             = "WORKFLOW.Api.ListWorkflows"
	ApiListWorkflowInstance      = "WORKFLOW.Api.ListWorkflowInstance"
	ApiGetWorkflowStatus         = "WORKFLOW.Api.GetWorkflowInstanceStatus"
	ApiCancelWorkflowInstance    = "WORKFLOW.Api.CancelWorkflowInstance"
	ApiSendMessage               = "WORKFLOW.Api.SendMessage"
	ApiCompleteManualTask        = "WORKFLOW.Api.CompleteManualTask"
	ApiCompleteServiceTask       = "WORKFLOW.Api.CompleteServiceTask"
	ApiCompleteUserTask          = "WORKFLOW.Api.CompleteUserTask"
	ApiListUserTaskIDs           = "WORKFLOW.Api.ListUserTaskIDs"
	ApiGetUserTask               = "WORKFLOW.Api.GetUserTask"
	ApiHandleWorkflowError       = "WORKFLOW.Api.HandleWorkflowError"
	ApiGetServerInstanceStats    = "WORKFLOW.Api.GetServerInstanceStats"
	ApiGetServiceTaskRoutingID   = "WORKFLOW.Api.getServiceTaskRoutingID"
	ApiGetMessageSenderRoutingID = "WORKFLOW.Api.getMEssageSenderRoutingID"
)

var (
	KvMessageSubs  = "WORKFLOW_MSGSUBS"
	KvMessageSub   = "WORKFLOW_MSGSUB"
	KvJob          = "WORKFLOW_JOB"
	KvVersion      = "WORKFLOW_VERSION"
	KvDefinition   = "WORKFLOW_DEF"
	KvTracking     = "WORKFLOW_TRACKING"
	KvInstance     = "WORKFLOW_INSTANCE"
	KvMessageName  = "WORKFLOW_MSGNAME"
	KvMessageID    = "WORKFLOW_MSGID"
	KvTrace        = "WORKFLOW_TRACKING"
	KvUserTask     = "WORKFLOW_USERTASK"
	KvOwnerName    = "WORKFLOW_OWNERNAME"
	KvOwnerId      = "WORKFLOW_OWNERID"
	KvClientTaskId = "WORKFLOW_CLIENTTASK"
	KvWfName       = "WORKFLOW_NAME"
)
