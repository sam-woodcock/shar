package output

import (
	"fmt"
	"github.com/pterm/pterm"
	"github.com/pterm/pterm/putils"
	"gitlab.com/shar-workflow/shar/common"
	"gitlab.com/shar-workflow/shar/model"
	"time"
)

// Console provides a client output implementation for console
type Console struct {
}

// OutputWorkflowInstanceStatus outputs a workflow instance status to console
func (c *Console) OutputWorkflowInstanceStatus(status []*model.WorkflowState) error {
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
	if err := pterm.DefaultTree.WithRoot(root).Render(); err != nil {
		return fmt.Errorf("error during render: %w", err)
	}
	return nil
}

func readStringPtr(ptr *string) string {
	if ptr == nil {
		return ""
	}

	return *ptr
}
