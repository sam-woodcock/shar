package list

import (
	"context"
	"fmt"
	"github.com/crystal-construct/shar/cli/flag"
	"github.com/crystal-construct/shar/cli/output"
	"github.com/crystal-construct/shar/client"
	"github.com/spf13/cobra"
)

var Cmd = &cobra.Command{
	Use:       "list",
	Short:     "List running workflow instances",
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
	res, err := shar.ListWorkflowInstance(ctx, wfName)
	if err != nil {
		return fmt.Errorf("failed to list workflows: %w", err)
	}
	for _, v := range res {
		fmt.Println("instance:", v.Id, "version: ", v.Version)
	}
	return nil
}
