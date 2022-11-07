package status

import (
	"context"
	"fmt"
	"github.com/spf13/cobra"
	"gitlab.com/shar-workflow/shar/cli/flag"
	"gitlab.com/shar-workflow/shar/cli/output"
	"gitlab.com/shar-workflow/shar/client"
)

var Cmd = &cobra.Command{
	Use:   "status",
	Short: "Gets the status of a running workflow instance",
	Long:  ``,
	RunE:  run,
	Args:  cobra.MatchAll(cobra.ExactArgs(1), cobra.OnlyValidArgs),
}

func run(cmd *cobra.Command, args []string) error {
	if err := cmd.ValidateArgs(args); err != nil {
		return err
	}
	ctx := context.Background()
	instanceID := args[0]
	shar := client.New(output.Logger)
	if err := shar.Dial(flag.Value.Server); err != nil {
		return fmt.Errorf("error dialling server: %w", err)
	}
	status, err := shar.GetWorkflowInstanceStatus(ctx, instanceID)
	if err != nil {
		return err
	}
	c := &output.Console{}
	return c.OutputWorkflowInstanceStatus(status)
}
