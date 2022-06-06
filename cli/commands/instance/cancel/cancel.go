/*
Copyright © 2022 NAME HERE <EMAIL ADDRESS>

*/
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

// rootCmd represents the base command when called without any subcommands
var Cmd = &cobra.Command{
	Use:   "cancel",
	Short: "Cancel a running workflow instance",
	Long:  ``,
	// Uncomment the following line if your bare application
	// has an action associated with it:
	RunE: run,
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

func init() {
	// Here you will define your flag and configuration settings.
	// Cobra supports persistent flag, which, if defined here,
	// will be global for your application.

	// rootCmd.PersistentFlags().StringVar(&cfgFile, "config", "", "config file (default is $HOME/.cli.yaml)")

	// Cobra also supports local flag, which will only run
	// when this action is called directly.
}
