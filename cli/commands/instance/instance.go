package instance

import (
	"github.com/spf13/cobra"
	"gitlab.com/shar-workflow/shar/cli/commands/instance/cancel"
	"gitlab.com/shar-workflow/shar/cli/commands/instance/list"
	"gitlab.com/shar-workflow/shar/cli/commands/instance/status"
)

// Cmd is the cobra command object
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
