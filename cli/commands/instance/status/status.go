package status

import (
	"context"
	"errors"
	"fmt"
	"github.com/crystal-construct/shar/cli/flag"
	"github.com/crystal-construct/shar/cli/output"
	"github.com/crystal-construct/shar/client"
	"github.com/spf13/cobra"
)

var Cmd = &cobra.Command{
	Use:   "status",
	Short: "Gets the status of a running workflow instance",
	Long:  ``,
	RunE:  run,
}

func run(cmd *cobra.Command, args []string) error {
	ctx := context.Background()
	if len(args) > 1 {
		return errors.New("too many arguments")
	}
	var wfName string
	if len(args) == 1 {
		wfName = args[0]
	}

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
