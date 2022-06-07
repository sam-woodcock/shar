package list

import (
	"context"
	"errors"
	"fmt"
	"github.com/crystal-construct/shar/cli/api"
	"github.com/crystal-construct/shar/cli/flag"
	"github.com/crystal-construct/shar/model"
	"github.com/spf13/cobra"
	"io"
)

var Cmd = &cobra.Command{
	Use:   "list",
	Short: "List running workflow instances",
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
	stream, err := shar.ListWorkflowInstance(ctx, &model.ListWorkflowInstanceRequest{WorkflowName: wfName})
	if err != nil {
		return fmt.Errorf("failed to list workflows: %w", err)
	}
	for {
		wfinf, err := stream.Recv()
		if err == io.EOF {
			break
		} else if err != nil {
			return err
		}
		fmt.Println("instance:", wfinf.Id, "version: ", wfinf.Version)
	}
	return nil
}
