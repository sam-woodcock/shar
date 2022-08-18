package list

import (
	"context"
	"fmt"
	"github.com/spf13/cobra"
	"gitlab.com/shar-workflow/shar/cli/flag"
	"gitlab.com/shar-workflow/shar/cli/output"
	"gitlab.com/shar-workflow/shar/client"
)

var Cmd = &cobra.Command{
	Use:   "list",
	Short: "Lists user tasks",
	Long:  ``,
	RunE:  run,
	Args:  cobra.ExactValidArgs(1),
}

func run(_ *cobra.Command, args []string) error {
	ctx := context.Background()
	shar := client.New(output.Logger)
	if err := shar.Dial(flag.Value.Server); err != nil {
		return fmt.Errorf("error dialling server: %w", err)
	}
	ut, err := shar.ListUserTaskIDs(ctx, args[0])
	if err != nil {
		return fmt.Errorf("send message failed: %w", err)
	}
	for i, v := range ut.Id {
		ut, _, err := shar.GetUserTask(ctx, args[0], v)
		if err != nil {
			return fmt.Errorf("failed to get user task %s: %w", v, err)
		}
		fmt.Println(i, ut)
	}
	return nil
}

func init() {
	Cmd.Flags().StringVarP(&flag.Value.CorrelationKey, flag.CorrelationKey, flag.CorrelationKeyShort, "", "a correlation key for the message")
}
