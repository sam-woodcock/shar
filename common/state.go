package common

import (
	"gitlab.com/shar-workflow/shar/model"
	"google.golang.org/protobuf/proto"
)

// CopyWorkflowState - clones a proto model.WorkflowState for modification.
func CopyWorkflowState(state *model.WorkflowState) *model.WorkflowState {
	return proto.Clone(state).(*model.WorkflowState)
}
