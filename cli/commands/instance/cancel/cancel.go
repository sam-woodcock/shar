package cancel

import (
	"context"
	"fmt"
	"github.com/crystal-construct/shar/cli/flag"
	"github.com/crystal-construct/shar/cli/output"
	"github.com/crystal-construct/shar/client"
	"github.com/spf13/cobra"
)

var Cmd = &cobra.Command{
	Use:       "cancel",
	Short:     "Cancel a running workflow instance",
	Long:      ``,
	RunE:      run,
	Args:      cobra.ExactValidArgs(1),
	ValidArgs: []string{"workflowInstanceId"},
}

func run(cmd *cobra.Command, args []string) error {
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
