package output

import (
	"fmt"
	"github.com/pterm/pterm"
	"github.com/pterm/pterm/putils"
	"gitlab.com/shar-workflow/shar/model"
	"time"
)

type Console struct {
}

func (c *Console) OutputWorkflowInstanceStatus(status []*model.WorkflowState) error {
	st := status[0]
	fmt.Println("Instance: " + st.WorkflowInstanceId)

	leveledList := pterm.LeveledList{
		pterm.LeveledListItem{Level: 0, Text: "Tracking ID: " + st.Id},
		pterm.LeveledListItem{Level: 1, Text: "Element"},
		pterm.LeveledListItem{Level: 2, Text: "ID: " + st.ElementId},
		pterm.LeveledListItem{Level: 2, Text: "Type: " + st.ElementType},
		pterm.LeveledListItem{Level: 1, Text: "State: " + st.State},
		pterm.LeveledListItem{Level: 1, Text: "Executing: " + *st.Execute},
		pterm.LeveledListItem{Level: 1, Text: "Since: " + time.Unix(0, st.UnixTimeNano).Format("“2006-01-02T15:04:05.999999999Z07:00”")},
	}
	root := putils.TreeFromLeveledList(leveledList)
	return pterm.DefaultTree.WithRoot(root).Render()
}
