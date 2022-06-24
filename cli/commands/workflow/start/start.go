package start

import (
	"context"
	"fmt"
	"gitlab.com/shar-workflow/shar/cli/flag"
	"gitlab.com/shar-workflow/shar/cli/output"
	"gitlab.com/shar-workflow/shar/client"
	"github.com/spf13/cobra"
)

var Cmd = &cobra.Command{
	Use:   "start",
	Short: "Starts a new workflow instance",
	Long:  `shar workflow start "WorkflowName"`,
	RunE:  run,
	Args:  cobra.ExactValidArgs(1),
}

func run(cmd *cobra.Command, args []string) error {
	ctx := context.Background()
	shar := client.New(output.Logger)
	if err := shar.Dial(flag.Value.Server); err != nil {
		return fmt.Errorf("error dialling server: %w", err)
	}
	wfiID, err := shar.LaunchWorkflow(ctx, args[0], nil)
	if err != nil {
		return fmt.Errorf("workflow launch failed: %w", err)
	}
	fmt.Println("workflow instance started. instance-id:", wfiID)
	return nil
}
