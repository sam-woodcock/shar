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
func (c *Text) OutputStartWorkflowResult(wfiID string) {
	fmt.Println("Workflow Instance:", wfiID)
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
func (c *Text) OutputWorkflowInstanceStatus(status []*model.WorkflowState) {
	st := status[0]
	fmt.Println("Instance: " + st.WorkflowInstanceId)

	leveledList := pterm.LeveledList{
		pterm.LeveledListItem{Level: 0, Text: "Tracking ID: " + common.TrackingID(st.Id).ID()},
		pterm.LeveledListItem{Level: 1, Text: "Element"},
		pterm.LeveledListItem{Level: 2, Text: "ID: " + st.ElementId},
		pterm.LeveledListItem{Level: 2, Text: "Type: " + st.ElementType},
		pterm.LeveledListItem{Level: 1, Text: "State: " + st.State.String()},
		pterm.LeveledListItem{Level: 1, Text: "Executing: " + readStringPtr(st.Execute)},
		pterm.LeveledListItem{Level: 1, Text: "Since: " + time.Unix(0, st.UnixTimeNano).Format("“2006-01-02T15:04:05.999999999Z07:00”")},
	}
	root := putils.TreeFromLeveledList(leveledList)
	op, err := pterm.DefaultTree.WithRoot(root).Srender()
	if err != nil {
		panic(fmt.Errorf("error during render: %w", err))
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
