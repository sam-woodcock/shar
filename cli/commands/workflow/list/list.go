package list

import (
	"context"
	"fmt"
	"github.com/crystal-construct/shar/cli/api"
	"github.com/crystal-construct/shar/cli/flag"
	"github.com/golang/protobuf/ptypes/empty"
	"github.com/spf13/cobra"
	"io"
)

var Cmd = &cobra.Command{
	Use:   "list",
	Short: "Lists available workflows",
	Long:  ``,
	RunE:  run,
}

func run(cmd *cobra.Command, args []string) error {
	ctx := context.Background()
	shar := api.New(api.Logger, flag.Value.Server)
	if err := shar.Dial(); err != nil {
		return fmt.Errorf("error dialling server: %w", err)
	}
	strm, err := shar.ListWorkflows(ctx, &empty.Empty{})
	if err != nil {
		return fmt.Errorf("list workflow failed: %w", err)
	}
	for {
		v, err := strm.Recv()
		if err != nil {
			if err == io.EOF {
				break
			}
			panic(err)
		}
		fmt.Println(v.Name, v.Version)
	}
	return nil
}
