package ctxkey

type sharContextKey string

var WorkflowInstanceID = sharContextKey("WORKFLOW_INSTANCE_ID")
