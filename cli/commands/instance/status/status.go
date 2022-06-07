package status

import (
	"context"
	"errors"
	"fmt"
	"github.com/crystal-construct/shar/cli/api"
	"github.com/crystal-construct/shar/cli/flag"
	"github.com/crystal-construct/shar/cli/output"
	"github.com/crystal-construct/shar/model"
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

	shar := api.New(api.Logger, flag.Value.Server)
	if err := shar.Dial(); err != nil {
		return fmt.Errorf("error dialling server: %w", err)
	}
	status, err := shar.GetWorkflowInstanceStatus(ctx, &model.GetWorkflowInstanceStatusRequest{Id: wfName})
	if err != nil {
		return err
	}
	c := &output.Console{}
	return c.OutputWorkflowInstanceStatus(status)
}
