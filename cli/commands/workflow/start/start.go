package start

import (
	"context"
	"fmt"
	"github.com/crystal-construct/shar/cli/api"
	"github.com/crystal-construct/shar/cli/flag"
	"github.com/crystal-construct/shar/model"
	"github.com/spf13/cobra"
)

var Cmd = &cobra.Command{
	Use:   "start",
	Short: "Starts a new workflow instance",
	Long:  ``,
	RunE:  run,
}

func run(cmd *cobra.Command, args []string) error {
	ctx := context.Background()
	shar := api.New(api.Logger, flag.Value.Server)
	if err := shar.Dial(); err != nil {
		return fmt.Errorf("error dialling server: %w", err)
	}
	wfiid, err := shar.LaunchWorkflow(ctx, &model.LaunchWorkflowRequest{
		Name: args[0],
		Vars: nil,
	})
	if err != nil {
		return fmt.Errorf("workflow launch failed: %w", err)
	}
	fmt.Println("workflow instance started. instance-id:", wfiid.Value)
	return nil
}
