package output

import (
	"fmt"
	"github.com/pterm/pterm"
	"github.com/pterm/pterm/putils"
	"gitlab.com/shar-workflow/shar/common"
	"gitlab.com/shar-workflow/shar/model"
	"time"
)

// Text provides a client output implementation for console
type Text struct {
}

// OutputStartWorkflowResult returns a CLI response
func (c *Text) OutputStartWorkflowResult(wfiID string, wfID string) {
	fmt.Println("Workflow Instance:", wfiID)
	fmt.Println("Workflow:", wfID)
}

// OutputWorkflow returns a CLI response
func (c *Text) OutputWorkflow(res []*model.ListWorkflowResult) {
	for _, v := range res {
		fmt.Println(v.Name, v.Version)
	}
}

// OutputListWorkflowInstance returns a CLI response
func (c *Text) OutputListWorkflowInstance(res []*model.ListWorkflowInstanceResult) {
	for i, v := range res {
		fmt.Println(i, ":", v.Id, "version", v.Version)
	}
}

// OutputUserTaskIDs returns a CLI response
func (c *Text) OutputUserTaskIDs(ut []*model.GetUserTaskResponse) {
	for i, v := range ut {
		fmt.Println(i, ": id:", v.TrackingId, "description:", v.Description)
	}
}

// OutputCancelledWorkflow returns a CLI response
func (c *Text) OutputCancelledWorkflow(id string) {
	fmt.Println("workflow", id, "cancelled.")
}

// OutputWorkflowInstanceStatus outputs a workflow instance status
func (c *Text) OutputWorkflowInstanceStatus(workflowInstanceID string, states map[string][]*model.WorkflowState) {
	fmt.Println("Instance: " + workflowInstanceID)

	ll := make(pterm.LeveledList, 0, len(states)*7+1)

	for p, sts := range states {
		ll = append(ll, pterm.LeveledListItem{Level: 0, Text: "Process ID: " + p})
		for _, st := range sts {
			ll = append(ll, pterm.LeveledListItem{Level: 0, Text: "Tracking ID: " + common.TrackingID(st.Id).ID()})
			ll = append(ll, pterm.LeveledListItem{Level: 1, Text: "Element"})
			ll = append(ll, pterm.LeveledListItem{Level: 2, Text: "ID: " + st.ElementId})
			ll = append(ll, pterm.LeveledListItem{Level: 2, Text: "Type: " + st.ElementType})
			ll = append(ll, pterm.LeveledListItem{Level: 1, Text: "State: " + st.State.String()})
			ll = append(ll, pterm.LeveledListItem{Level: 1, Text: "Executing: " + readStringPtr(st.Execute)})
			ll = append(ll, pterm.LeveledListItem{Level: 1, Text: "Since: " + time.Unix(0, st.UnixTimeNano).Format("“2006-01-02T15:04:05.999999999Z07:00”")})
		}
	}
	root := putils.TreeFromLeveledList(ll)
	op, err := pterm.DefaultTree.WithRoot(root).Srender()
	if err != nil {
		panic(fmt.Errorf("render: %w", err))
	}
	if _, err := Stream.Write([]byte(op)); err != nil {
		panic(err)
	}
}

// OutputLoadResult returns a CLI response
func (c *Text) OutputLoadResult(workflowInstanceID string) {
	fmt.Println("Instance: " + workflowInstanceID)
}

func readStringPtr(ptr *string) string {
	if ptr == nil {
		return ""
	}

	return *ptr
}
