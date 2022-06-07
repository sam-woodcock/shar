package instance

import (
	"github.com/crystal-construct/shar/cli/commands/instance/cancel"
	"github.com/crystal-construct/shar/cli/commands/instance/list"
	"github.com/crystal-construct/shar/cli/commands/instance/status"
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
