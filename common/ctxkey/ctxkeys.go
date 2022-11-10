package ctxkey

type sharContextKey string

// WorkflowInstanceID is the context key for the workflow instance ID
var WorkflowInstanceID = sharContextKey("WORKFLOW_INSTANCE_ID")

// TrackingID is the context key for the workflow tracking ID
var TrackingID = sharContextKey("WF_TRACKING_ID")
