package output

import (
	"gitlab.com/shar-workflow/shar/model"
	"io"
	"os"
)

// Method represents the output method
type Method interface {
	OutputWorkflowInstanceStatus(workflowInstanceID string, states map[string][]*model.WorkflowState)
	OutputLoadResult(workflowInstanceID string)
	OutputListWorkflowInstance(res []*model.ListWorkflowInstanceResult)
	OutputCancelledWorkflow(id string)
	OutputUserTaskIDs(ut []*model.GetUserTaskResponse)
	OutputWorkflow(res []*model.ListWorkflowResult)
	OutputStartWorkflowResult(wfiID string, wfID string)
}

// Current is the currently selected output method.
var Current Method

// Stream contains the output stream.  By default this os.Stdout, however, for testing it can be set to a byte buffer for instance.
var Stream io.Writer = os.Stdout
