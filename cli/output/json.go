package output

import (
	"encoding/json"
	"gitlab.com/shar-workflow/shar/common"
	"gitlab.com/shar-workflow/shar/model"
)

// Json contains the output methods for returning json CLI responses
type Json struct {
}

// OutputStartWorkflowResult returns a CLI response
func (c *Json) OutputStartWorkflowResult(wfiID string, wfID string) {
	outJson(struct {
		WorkflowInstanceID string
		WorkflowID         string
	}{
		WorkflowInstanceID: wfiID,
		WorkflowID:         wfID,
	})
}

// OutputWorkflow returns a CLI response
func (c *Json) OutputWorkflow(res []*model.ListWorkflowResult) {
	outJson(struct {
		Workflow []*model.ListWorkflowResult
	}{
		Workflow: res,
	})
}

// OutputListWorkflowInstance returns a CLI response
func (c *Json) OutputListWorkflowInstance(res []*model.ListWorkflowInstanceResult) {
	outJson(struct {
		WorkflowInstance []*model.ListWorkflowInstanceResult
	}{
		WorkflowInstance: res,
	})
}

// OutputUserTaskIDs returns a CLI response
func (c *Json) OutputUserTaskIDs(ut []*model.GetUserTaskResponse) {
	outJson(struct {
		UserTasks []*model.GetUserTaskResponse
	}{
		UserTasks: ut,
	})
}

// OutputWorkflowInstanceStatus outputs a workflow instance status to console
func (c *Json) OutputWorkflowInstanceStatus(workflowInstanceID string, states map[string][]*model.WorkflowState) {
	type retState struct {
		TrackingId string
		ID         string
		Type       string
		State      string
		Executing  string
		Since      int64
	}

	type retInstance struct {
		InstanceId string
		Processes  map[string][]retState
	}

	rs := make(map[string][]retState, len(states))
	for pi, sts := range states {
		rsa := make([]retState, 0, len(sts))
		for _, st := range sts {
			rsa = append(rsa, retState{
				TrackingId: common.TrackingID(st.Id).ID(),
				ID:         st.ElementId,
				Type:       st.ElementType,
				State:      st.State.String(),
				Executing:  readStringPtr(st.Execute),
				Since:      st.UnixTimeNano,
			})
		}
		rs[pi] = rsa
	}
	outJson(retInstance{InstanceId: workflowInstanceID, Processes: rs})
}

// OutputLoadResult returns a CLI response
func (c *Json) OutputLoadResult(workflowInstanceID string) {
	outJson(struct {
		WorkflowID string
	}{
		WorkflowID: workflowInstanceID,
	})
}

// OutputCancelledWorkflow returns a CLI response
func (c *Json) OutputCancelledWorkflow(id string) {
	outJson(struct {
		Cancelled string
	}{
		Cancelled: id,
	})
}

func outJson(js interface{}) {
	op, err := json.Marshal(&js)
	if err != nil {
		panic(err)
	}
	if _, err := Stream.Write(op); err != nil {
		panic(err)
	}
}
