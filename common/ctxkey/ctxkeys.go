package ctxkey

type SharContextKey string

var WorkflowInstanceID = SharContextKey("WORKFLOW_INSTANCE_ID")
