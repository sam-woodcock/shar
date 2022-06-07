package output

import (
	"fmt"
	"github.com/crystal-construct/shar/model"
	"github.com/pterm/pterm"
	"time"
)

type Console struct {
}

func (c *Console) OutputWorkflowInstanceStatus(status *model.WorkflowInstanceStatus) error {
	st := status.State[0]
	fmt.Println("Instance: " + st.WorkflowInstanceId)

	leveledList := pterm.LeveledList{
		pterm.LeveledListItem{Level: 0, Text: "Tracking ID: " + st.TrackingId},
		pterm.LeveledListItem{Level: 1, Text: "Element"},
		pterm.LeveledListItem{Level: 2, Text: "ID: " + st.ElementId},
		pterm.LeveledListItem{Level: 2, Text: "Type: " + st.ElementType},
		pterm.LeveledListItem{Level: 1, Text: "State: " + st.State},
		pterm.LeveledListItem{Level: 1, Text: "Executing: " + st.Execute},
		pterm.LeveledListItem{Level: 1, Text: "Since: " + time.Unix(0, st.UnixTimeNano).Format("“2006-01-02T15:04:05.999999999Z07:00”")},
	}
	root := pterm.NewTreeFromLeveledList(leveledList)
	return pterm.DefaultTree.WithRoot(root).Render()
}
