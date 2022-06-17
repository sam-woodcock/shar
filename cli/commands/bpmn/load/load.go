package load

import (
	"context"
	"fmt"
	"github.com/crystal-construct/shar/cli/flag"
	"github.com/crystal-construct/shar/cli/output"
	"github.com/crystal-construct/shar/client"
	"github.com/pkg/errors"
	"github.com/spf13/cobra"
	"os"
)

var Cmd = &cobra.Command{
	Use:   "load",
	Short: "Loads a BPMN XML file into shar",
	Long: `shar bpmn load "WorkflowName" ./path-to-workflow.bpmn 
	`,
	RunE: run,
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
	wn, err := shar.LoadBMPNWorkflowFromBytes(ctx, args[0], b)
	if err != nil {
		return fmt.Errorf("workflow could not be loaded: %w", err)
	}
	fmt.Println("workflow " + wn + " loaded.")
	return nil
}
