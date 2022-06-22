package status

import (
	"context"
	"fmt"
	"github.com/crystal-construct/shar/cli/flag"
	"github.com/crystal-construct/shar/cli/output"
	"github.com/crystal-construct/shar/client"
	"github.com/spf13/cobra"
)

var Cmd = &cobra.Command{
	Use:       "status",
	Short:     "Gets the status of a running workflow instance",
	Long:      ``,
	RunE:      run,
	Args:      cobra.ExactValidArgs(1),
	ValidArgs: []string{"workflowName"},
}

func run(cmd *cobra.Command, args []string) error {
	ctx := context.Background()
	wfName := args[0]
	shar := client.New(output.Logger)
	if err := shar.Dial(flag.Value.Server); err != nil {
		return fmt.Errorf("error dialling server: %w", err)
	}
	status, err := shar.GetWorkflowInstanceStatus(ctx, wfName)
	if err != nil {
		return err
	}
	c := &output.Console{}
	return c.OutputWorkflowInstanceStatus(status)
}
