package load

import (
	"context"
	"errors"
	"fmt"
	"github.com/spf13/cobra"
	"gitlab.com/shar-workflow/shar/cli/flag"
	"gitlab.com/shar-workflow/shar/cli/output"
	"gitlab.com/shar-workflow/shar/client"
	"os"
)

var Cmd = &cobra.Command{
	Use:   "load",
	Short: "Loads a BPMN XML file into shar",
	Long: `shar bpmn load "WorkflowName" ./path-to-workflow.bpmn 
	`,
	RunE: run,
	Args: cobra.ExactValidArgs(1),
}

func run(cmd *cobra.Command, args []string) error {
	ctx := context.Background()
	if len(args) == 0 {
		return errors.New("no file provided")
	}
	b, err := os.ReadFile(args[1])
	if err != nil {
		return fmt.Errorf("error reading file: %w", err)
	}

	shar := client.New(output.Logger)
	if err := shar.Dial(flag.Value.Server); err != nil {
		return fmt.Errorf("error dialling server: %w", err)
	}
	wn, err := shar.LoadBPMNWorkflowFromBytes(ctx, args[0], b)
	if err != nil {
		return fmt.Errorf("workflow could not be loaded: %w", err)
	}
	fmt.Println("workflow " + wn + " loaded.")
	return nil
}
