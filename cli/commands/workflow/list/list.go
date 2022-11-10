package list

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
	Use:   "list",
	Short: "Lists available workflows",
	Long:  ``,
	RunE:  run,
	Args:  cobra.NoArgs,
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
	res, err := shar.ListWorkflows(ctx)
	if err != nil {
		return fmt.Errorf("list workflow failed: %w", err)
	}
	for _, v := range res {
		fmt.Println(v.Name, v.Version)
	}
	return nil
}
