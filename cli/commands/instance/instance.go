package instance

import (
	"gitlab.com/shar-workflow/shar/cli/commands/instance/cancel"
	"gitlab.com/shar-workflow/shar/cli/commands/instance/list"
	"gitlab.com/shar-workflow/shar/cli/commands/instance/status"
	"github.com/spf13/cobra"
)

var Cmd = &cobra.Command{
	Use:   "instance",
	Short: "Commands for dealing with workflow instances",
	Long:  ``,
}

func init() {
	Cmd.AddCommand(list.Cmd)
	Cmd.AddCommand(status.Cmd)
	Cmd.AddCommand(cancel.Cmd)
}
