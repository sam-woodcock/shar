package send

import (
	"context"
	"fmt"
	"github.com/spf13/cobra"
	"gitlab.com/shar-workflow/shar/cli/flag"
	"gitlab.com/shar-workflow/shar/cli/output"
	"gitlab.com/shar-workflow/shar/client"
	"gitlab.com/shar-workflow/shar/model"
)

var Cmd = &cobra.Command{
	Use:   "send",
	Short: "Sends a workflow message",
	Long:  ``,
	RunE:  run,
	Args:  cobra.MatchAll(cobra.ExactArgs(2), cobra.OnlyValidArgs),
}

func run(cmd *cobra.Command, args []string) error {
	if err := cmd.ValidateArgs(args); err != nil {
		return err
	}
	ctx := context.Background()
	shar := client.New(output.Logger)
	if err := shar.Dial(flag.Value.Server); err != nil {
		return fmt.Errorf("error dialling server: %w", err)
	}
	err := shar.SendMessage(ctx, args[0], args[1], flag.Value.CorrelationKey, model.Vars{})
	if err != nil {
		return fmt.Errorf("send message failed: %w", err)
	}
	return nil
}

func init() {
	Cmd.Flags().StringVarP(&flag.Value.CorrelationKey, flag.CorrelationKey, flag.CorrelationKeyShort, "", "a correlation key for the message")
}
