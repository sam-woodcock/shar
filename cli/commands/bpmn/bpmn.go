package bpmn

import (
	"github.com/crystal-construct/shar/cli/commands/bpmn/load"
	"github.com/spf13/cobra"
)

var Cmd = &cobra.Command{
	Use:   "bpmn",
	Short: "Actions for manipulating bpmn",
	Long:  ``,
}

func init() {
	Cmd.AddCommand(load.Cmd)
}
