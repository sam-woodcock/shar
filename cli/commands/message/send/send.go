package send

import (
	"context"
	"fmt"
	"gitlab.com/shar-workflow/shar/cli/flag"
	"gitlab.com/shar-workflow/shar/cli/output"
	"gitlab.com/shar-workflow/shar/client"
	"github.com/spf13/cobra"
)

var Cmd = &cobra.Command{
	Use:   "send",
	Short: "Sends a workflow message",
	Long:  ``,
	RunE:  run,
	Args:  cobra.ExactValidArgs(2),
}

func run(cmd *cobra.Command, args []string) error {
	ctx := context.Background()
	shar := client.New(output.Logger)
	if err := shar.Dial(flag.Value.Server); err != nil {
		return fmt.Errorf("error dialling server: %w", err)
	}
	err := shar.SendMessage(ctx, args[0], args[1], flag.Value.CorrelationKey)
	if err != nil {
		return fmt.Errorf("send message failed: %w", err)
	}
	return nil
}

func init() {
	Cmd.Flags().StringVarP(&flag.Value.CorrelationKey, flag.CorrelationKey, flag.CorrelationKeyShort, "", "a correlation key for the message")
}
