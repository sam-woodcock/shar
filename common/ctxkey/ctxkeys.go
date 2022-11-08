package ctxkey

type sharContextKey string

var WorkflowInstanceID = sharContextKey("WORKFLOW_INSTANCE_ID")
var TrackingID = sharContextKey("WF_TRACKING_ID")
var Client = sharContextKey("WF_CLIENT")
