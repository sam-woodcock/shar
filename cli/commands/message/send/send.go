package send

import (
	"context"
	"fmt"
	"github.com/crystal-construct/shar/cli/flag"
	"github.com/crystal-construct/shar/cli/output"
	"github.com/crystal-construct/shar/client"
	"github.com/spf13/cobra"
)

var Cmd = &cobra.Command{
	Use:   "send",
	Short: "Sends a workflow message",
	Long:  ``,
	RunE:  run,
}

func run(cmd *cobra.Command, args []string) error {
	ctx := context.Background()
	shar := client.New(output.Logger)
	if err := shar.Dial(flag.Value.Server); err != nil {
		return fmt.Errorf("error dialling server: %w", err)
	}
	err := shar.SendMessage(ctx, args[0], flag.Value.CorrelationKey)
	if err != nil {
		return fmt.Errorf("send message failed: %w", err)
	}
	return nil
}

func init() {
	Cmd.Flags().StringVarP(&flag.Value.CorrelationKey, flag.CorrelationKey, flag.CorrelationKeyShort, "", "a correlation key for the message")

}
