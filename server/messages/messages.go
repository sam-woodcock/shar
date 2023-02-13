package messages

import "gitlab.com/shar-workflow/shar/common/subj"

const (
	WorkFlowJobAbortAll               = "WORKFLOW.%s.State.Job.Abort.*"               // WorkFlowJobAbortAll is the wildcard state message subject for all job abort messages.
	WorkFlowJobCompleteAll            = "WORKFLOW.%s.State.Job.Complete.*"            // WorkFlowJobCompleteAll is the wildcard state message subject for all job completion messages.
	WorkflowActivityAbort             = "WORKFLOW.%s.State.Activity.Abort"            // WorkflowActivityAbort is the state message subject for aborting an activity.
	WorkflowActivityAll               = "WORKFLOW.%s.State.Activity.>"                // WorkflowActivityAll is the wildcard state message subject for all activity messages.
	WorkflowActivityComplete          = "WORKFLOW.%s.State.Activity.Complete"         // WorkflowActivityComplete is the state message subject for completing an activity.
	WorkflowActivityExecute           = "WORKFLOW.%s.State.Activity.Execute"          // WorkflowActivityExecute is the state message subject for executing an activity.
	WorkflowCommands                  = "WORKFLOW.%s.Command.>"                       // WorkflowCommands is the wildcard state message subject for all workflow commands.
	WorkflowElementTimedExecute       = "WORKFLOW.%s.Timers.ElementExecute"           // WorkflowElementTimedExecute is the state message subject for a timed element execute operation.
	WorkflowGeneralAbortAll           = "WORKFLOW.%s.State.*.Abort"                   // WorkflowGeneralAbortAll is the wildcard state message subject for all abort messages/.
	WorkflowInstanceAbort             = "WORKFLOW.%s.State.Workflow.Abort"            // WorkflowInstanceAbort is the state message subject for a workflow instace being aborted.
	WorkflowInstanceAll               = "WORKFLOW.%s.State.Workflow.>"                // WorkflowInstanceAll is the wildcard state message subject for all workflow state messages.
	WorkflowInstanceComplete          = "WORKFLOW.%s.State.Workflow.Complete"         // WorkflowInstanceComplete is the state message subject for completing a workfloe instance.
	WorkflowInstanceExecute           = "WORKFLOW.%s.State.Workflow.Execute"          // WorkflowInstanceExecute is the state message subject for executing a workflow instance.
	WorkflowInstanceTerminated        = "WORKFLOW.%s.State.Workflow.Terminated"       // WorkflowInstanceTerminated is the state message subject for a workflow instance terminating.
	WorkflowJobLaunchComplete         = "WORKFLOW.%s.State.Job.Complete.Launch"       // WorkflowJobLaunchComplete is the state message subject for completing a launch subworkflow task.
	WorkflowJobLaunchExecute          = "WORKFLOW.%s.State.Job.Execute.Launch"        // WorkflowJobLaunchExecute is the state message subject for executing a launch subworkflow task.
	WorkflowJobManualTaskAbort        = "WORKFLOW.%s.State.Job.Abort.ManualTask"      // WorkflowJobManualTaskAbort is the state message subject for sborting a manual task.
	WorkflowJobManualTaskComplete     = "WORKFLOW.%s.State.Job.Complete.ManualTask"   // WorkflowJobManualTaskComplete is the state message subject for completing a manual task.
	WorkflowJobManualTaskExecute      = "WORKFLOW.%s.State.Job.Execute.ManualTask"    // WorkflowJobManualTaskExecute is the state message subject for executing a manual task.
	WorkflowJobSendMessageComplete    = "WORKFLOW.%s.State.Job.Complete.SendMessage"  // WorkflowJobSendMessageComplete is the state message subject for completing a send message task.
	WorkflowJobSendMessageExecute     = "WORKFLOW.%s.State.Job.Execute.SendMessage"   // WorkflowJobSendMessageExecute is the state message subject for executing a send workfloe message task.
	WorkflowJobSendMessageExecuteWild = "WORKFLOW.%s.State.Job.Execute.SendMessage.>" // WorkflowJobSendMessageExecuteWild is the wildcard state message subject for executing a send workfloe message task.
	WorkflowJobServiceTaskAbort       = "WORKFLOW.%s.State.Job.Abort.ServiceTask"     // WorkflowJobServiceTaskAbort is the state message subject for aborting an in progress service task.
	WorkflowJobServiceTaskComplete    = "WORKFLOW.%s.State.Job.Complete.ServiceTask"  // WorkflowJobServiceTaskComplete is the state message subject for a completed service task,
	WorkflowJobServiceTaskExecute     = "WORKFLOW.%s.State.Job.Execute.ServiceTask"   // WorkflowJobServiceTaskExecute is the raw state message subject for executing a service task.  An identifier is added to the end to route messages to the clients.
	WorkflowJobServiceTaskExecuteWild = "WORKFLOW.%s.State.Job.Execute.ServiceTask.>" // WorkflowJobServiceTaskExecuteWild is the wildcard state message subject for all execute service task messages.
	WorkflowJobTimerTaskComplete      = "WORKFLOW.%s.State.Job.Complete.Timer"        // WorkflowJobTimerTaskComplete is the state message subject for completing a timed task.
	WorkflowJobTimerTaskExecute       = "WORKFLOW.%s.State.Job.Execute.Timer"         // WorkflowJobTimerTaskExecute is the state message subject for executing a timed task.
	WorkflowJobUserTaskAbort          = "WORKFLOW.%s.State.Job.Abort.UserTask"        // WorkflowJobUserTaskAbort is the state message subject for aborting a user task.
	WorkflowJobUserTaskComplete       = "WORKFLOW.%s.State.Job.Complete.UserTask"     // WorkflowJobUserTaskComplete is the state message subject for completing a user task.
	WorkflowJobUserTaskExecute        = "WORKFLOW.%s.State.Job.Execute.UserTask"      // WorkflowJobUserTaskExecute is the state message subject for executing a user task.
	WorkflowLog                       = "WORKFLOW.%s.State.Log"                       // WorkflowLog is the state message subject for logging messages to a workflow activity.
	WorkflowLogAll                    = "WORKFLOW.%s.State.Log.*"                     // WorkflowLogAll is the wildcard state message subject for all logging messages.
	WorkflowMessages                  = "WORKFLOW.%s.Message.>"                       // WorkflowMessages is the wildcard state message subject for all workflow messages.
	WorkflowProcessComplete           = "WORKFLOW.%s.State.Process.Complete"          // WorkflowProcessComplete is the state message subject for completing a workfloe process.
	WorkflowProcessExecute            = "WORKFLOW.%s.State.Process.Execute"           // WorkflowProcessExecute is the state message subject for executing a workflow process.
	WorkflowProcessTerminated         = "WORKFLOW.%s.State.Process.Terminated"        // WorkflowProcessTerminated is the state message subject for a workflow process terminating.
	WorkflowStateAll                  = "WORKFLOW.%s.State.>"                         // WorkflowStateAll is the wildcard subject for catching all state messages.
	WorkflowTimedExecute              = "WORKFLOW.%s.Timers.WorkflowExecute"          // WorkflowTimedExecute is the state message subject for timed workflow execute operation.
	WorkflowTraversalComplete         = "WORKFLOW.%s.State.Traversal.Complete"        // WorkflowTraversalComplete is the state message subject for completing a traversal.
	WorkflowTraversalExecute          = "WORKFLOW.%s.State.Traversal.Execute"         // WorkflowTraversalExecute is the state message subject for executing a new traversal.
)

// WorkflowLogLevel represents a subject suffox for logging levels
type WorkflowLogLevel string

const (
	LogFatal WorkflowLogLevel = ".Fatal"   // LogFatal is the suffix for a fatal error.
	LogError WorkflowLogLevel = ".Error"   // LogError is the suffix for an error.
	LogWarn  WorkflowLogLevel = ".Warning" // LogWarn is the suffix for a warning.
	LogInfo  WorkflowLogLevel = ".Info"    // LogInfo is the suffix for an information message.
	LogDebug WorkflowLogLevel = ".Debug"   // LogDebug is the suffix for a debug message.
)

// LogLevels provides a way of using an index to select a log level.
var LogLevels = []WorkflowLogLevel{
	LogFatal,
	LogError,
	LogWarn,
	LogInfo,
	LogDebug,
}

// AllMessages provides the list of subscriptions for the WORKFLOW stream.
var AllMessages = []string{
	//subj.NS(WorkflowAbortAll, "*"),
	subj.NS(WorkFlowJobAbortAll, "*"),
	subj.NS(WorkFlowJobCompleteAll, "*"),
	subj.NS(WorkflowActivityAbort, "*"),
	subj.NS(WorkflowActivityComplete, "*"),
	subj.NS(WorkflowActivityExecute, "*"),
	subj.NS(WorkflowCommands, "*"),
	subj.NS(WorkflowElementTimedExecute, "*"),
	subj.NS(WorkflowInstanceAll, "*"),
	subj.NS(WorkflowJobLaunchExecute, "*"),
	subj.NS(WorkflowJobManualTaskExecute, "*"),
	subj.NS(WorkflowJobSendMessageExecuteWild, "*"),
	subj.NS(WorkflowJobServiceTaskExecuteWild, "*"),
	subj.NS(WorkflowJobTimerTaskExecute, "*"),
	subj.NS(WorkflowJobUserTaskExecute, "*"),
	subj.NS(WorkflowLogAll, "*"),
	subj.NS(WorkflowMessages, "*"),
	subj.NS(WorkflowProcessComplete, "*"),
	subj.NS(WorkflowProcessExecute, "*"),
	subj.NS(WorkflowProcessTerminated, "*"),
	subj.NS(WorkflowTimedExecute, "*"),
	subj.NS(WorkflowTraversalComplete, "*"),
	subj.NS(WorkflowTraversalExecute, "*"),
}

// WorkflowMessageFormat provides the template for sending workflow messages.
var WorkflowMessageFormat = "WORKFLOW.%s.Message.%s.%s"

const (
	APIAll                           = "WORKFLOW.Api.*"                             // APIAll is all API message subjects.
	APIStoreWorkflow                 = "WORKFLOW.Api.StoreWorkflow"                 // APIStoreWorkflow is the store Workflow API subject.
	APILaunchWorkflow                = "WORKFLOW.Api.LaunchWorkflow"                // APILaunchWorkflow is the launch workflow API subject.
	APIListWorkflows                 = "WORKFLOW.Api.ListWorkflows"                 // APIListWorkflows is the list workflows API subject.
	APIListWorkflowInstance          = "WORKFLOW.Api.ListWorkflowInstance"          // APIListWorkflowInstance is the list workflow instances API subject.
	APIListWorkflowInstanceProcesses = "WORKFLOW.Api.ListWorkflowInstanceProcesses" // APIListWorkflowInstanceProcesses is the get processes of a running workflow instance API subject.
	APICancelWorkflowInstance        = "WORKFLOW.Api.CancelWorkflowInstance"        // APICancelWorkflowInstance is the cancel a workflow instance API subject.
	APISendMessage                   = "WORKFLOW.Api.SendMessage"                   // APISendMessage is the send workflow message API subject.
	APICompleteManualTask            = "WORKFLOW.Api.CompleteManualTask"            // APICompleteManualTask is the complete manual task API subject.
	APICompleteServiceTask           = "WORKFLOW.Api.CompleteServiceTask"           // APICompleteServiceTask is the complete service task API subject.
	APICompleteUserTask              = "WORKFLOW.Api.CompleteUserTask"              // APICompleteUserTask is the complete user task API subject.
	APICompleteSendMessageTask       = "WORKFLOW.Api.CompleteSendMessageTask"       // APICompleteSendMessageTask is the complete send message task API subject.
	APIListUserTaskIDs               = "WORKFLOW.Api.ListUserTaskIDs"               // APIListUserTaskIDs is the list user task IDs API subject.
	APIGetUserTask                   = "WORKFLOW.Api.GetUserTask"                   // APIGetUserTask is the get user task API subject.
	APIHandleWorkflowError           = "WORKFLOW.Api.HandleWorkflowError"           // APIHandleWorkflowError is the handle workflow error API subject.
	APIGetServerInstanceStats        = "WORKFLOW.Api.GetServerInstanceStats"        // APIGetServerInstanceStats is the get server instance status API subject.
	APIGetServiceTaskRoutingID       = "WORKFLOW.Api.GetServiceTaskRoutingID"       // APIGetServiceTaskRoutingID is the get client routing ID for a service task API subject.
	APIGetMessageSenderRoutingID     = "WORKFLOW.Api.GetMessageSenderRoutingID"     // APIGetMessageSenderRoutingID is the get message sender routing ID API subject.
	APIGetProcessInstanceStatus      = "WORKFLOW.Api.GetProcessInstanceStatus"      // APIGetProcessInstanceStatus is the get process instance status API subject.
	APIGetWorkflowVersions           = "WORKFLOW.Api.GetWorkflowVersions"           // APIGetWorkflowVersions is the get workflow versions API message subject.
	APIGetWorkflow                   = "WORKFLOW.Api.GetWorkflow"                   // APIGetWorkflow is the get workflow API message subject.
)

var (
	KvMessageSubs     = "WORKFLOW_MSGSUBS"    // KvMessageSubs is the name of the key value store that holds the list of message subscriber IDs for a workflow message.
	KvMessageSub      = "WORKFLOW_MSGSUB"     // KvMessageSub is the name of the key value store that holds each message subscriber.
	KvJob             = "WORKFLOW_JOB"        // KvJob is the name of the key value store that holds workflow jobs.
	KvVersion         = "WORKFLOW_VERSION"    // KvVersion is the name of the key value store that holds an ordered list of workflow version IDs for a given workflow
	KvDefinition      = "WORKFLOW_DEF"        // KvDefinition is the name of the key value store that holds the state machine definition for workflows
	KvTracking        = "WORKFLOW_TRACKING"   // KvTracking is the name of the key value store that holds the state of a workflow task.
	KvInstance        = "WORKFLOW_INSTANCE"   // KvInstance is the name of the key value store that holds workflow instance information.
	KvMessageName     = "WORKFLOW_MSGNAME"    // KvMessageName is the name of the key value store that holds message IDs for message names.
	KvMessageID       = "WORKFLOW_MSGID"      // KvMessageID is the name of the key value store that holds message names for message IDs
	KvUserTask        = "WORKFLOW_USERTASK"   // KvUserTask is the name of the key value store that holds active user tasks.
	KvOwnerName       = "WORKFLOW_OWNERNAME"  // KvOwnerName is the name of the key value store that holds owner names for owner IDs
	KvOwnerID         = "WORKFLOW_OWNERID"    // KvOwnerID is the name of the key value store that holds owner IDs for owner names.
	KvClientTaskID    = "WORKFLOW_CLIENTTASK" // KvClientTaskID is the name of the key value store that holds the unique ID used by clients to subscribe to service task messages.
	KvWfName          = "WORKFLOW_NAME"       // KvWfName is the name of the key value store that holds workflow IDs for workflow names.
	KvVarState        = "WORKFLOW_VARSTATE"   // KvVarState is the name of the key value store that holds the state of variables upon entering a task.
	KvProcessInstance = "WORKFLOW_PROCESS"    // KvProcessInstance is the name of the key value store holding process instances.
)
