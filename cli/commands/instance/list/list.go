package list

import (
	"context"
	"fmt"
	"github.com/spf13/cobra"
	"gitlab.com/shar-workflow/shar/cli/flag"
	"gitlab.com/shar-workflow/shar/cli/output"
	"gitlab.com/shar-workflow/shar/client"
)

var Cmd = &cobra.Command{
	Use:   "list",
	Short: "List running workflow instances",
	Long:  ``,
	RunE:  run,
	Args:  cobra.MatchAll(cobra.ExactArgs(1), cobra.OnlyValidArgs),
}

func run(_ *cobra.Command, args []string) error {
	ctx := context.Background()
	wfName := args[0]
	shar := client.New(output.Logger)
	if err := shar.Dial(flag.Value.Server); err != nil {
		return fmt.Errorf("error dialling server: %w", err)
	}
	res, err := shar.ListWorkflowInstance(ctx, wfName)
	if err != nil {
		return fmt.Errorf("failed to list workflows: %w", err)
	}
	for _, v := range res {
		fmt.Println("instance:", v.Id, "version: ", v.Version)
	}
	return nil
}
