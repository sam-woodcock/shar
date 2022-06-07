package cancel

import (
	"context"
	"fmt"
	"github.com/crystal-construct/shar/cli/api"
	"github.com/crystal-construct/shar/cli/flag"
	"github.com/crystal-construct/shar/model"
	"github.com/pkg/errors"
	"github.com/spf13/cobra"
)

var Cmd = &cobra.Command{
	Use:   "cancel",
	Short: "Cancel a running workflow instance",
	Long:  ``,
	RunE:  run,
}

func run(cmd *cobra.Command, args []string) error {
	ctx := context.Background()
	if len(args) > 1 {
		return errors.New("too many arguments")
	}
	var wfiid string
	if len(args) == 1 {
		wfiid = args[0]
	}

	shar := api.New(api.Logger, flag.Value.Server)
	if err := shar.Dial(); err != nil {
		return fmt.Errorf("error dialling server: %w", err)
	}
	_, err := shar.CancelWorkflowInstance(ctx, &model.CancelWorkflowInstanceRequest{Id: wfiid})
	if err != nil {
		return fmt.Errorf("failed to cancel workflow instance: %w", err)
	}
	fmt.Println("workflow", wfiid, "cancelled.")
	return nil
}
