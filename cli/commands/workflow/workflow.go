package workflow

import (
	"gitlab.com/shar-workflow/shar/cli/commands/workflow/list"
	"gitlab.com/shar-workflow/shar/cli/commands/workflow/start"
	"github.com/spf13/cobra"
)

var Cmd = &cobra.Command{
	Use:   "workflow",
	Short: "Commands for dealing with workflows",
	Long:  ``,
}

func init() {
	Cmd.AddCommand(start.Cmd)
	Cmd.AddCommand(list.Cmd)
}
