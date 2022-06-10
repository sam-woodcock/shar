package message

import (
	"github.com/crystal-construct/shar/cli/commands/message/send"
	"github.com/spf13/cobra"
)

var Cmd = &cobra.Command{
	Use:   "message",
	Short: "Commands for sending workflow messages",
	Long:  ``,
}

func init() {
	Cmd.AddCommand(send.Cmd)
}
