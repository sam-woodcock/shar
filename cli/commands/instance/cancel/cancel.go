package cancel

import (
	"context"
	"fmt"
	"github.com/spf13/cobra"
	"gitlab.com/shar-workflow/shar/cli/flag"
	"gitlab.com/shar-workflow/shar/cli/output"
	"gitlab.com/shar-workflow/shar/client"
)

// Cmd is the cobra command object
var Cmd = &cobra.Command{
	Use:   "cancel",
	Short: "Cancel a running workflow instance",
	Long:  ``,
	RunE:  run,
	Args:  cobra.MatchAll(cobra.ExactArgs(1), cobra.OnlyValidArgs),
}

func run(cmd *cobra.Command, args []string) error {
	if err := cmd.ValidateArgs(args); err != nil {
		return err
	}
	ctx := context.Background()
	wfiID := args[0]

	shar := client.New(output.Logger)
	if err := shar.Dial(flag.Value.Server); err != nil {
		return fmt.Errorf("error dialling server: %w", err)
	}
	if err := shar.CancelWorkflowInstance(ctx, wfiID); err != nil {
		return fmt.Errorf("failed to cancel workflow instance: %w", err)
	}
	fmt.Println("workflow", wfiID, "cancelled.")
	return nil
}
