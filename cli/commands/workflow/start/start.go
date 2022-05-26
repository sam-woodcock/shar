/*
Copyright © 2022 NAME HERE <EMAIL ADDRESS>

*/
package start

import (
	"context"
	"fmt"
	"github.com/crystal-construct/shar/cli/api"
	"github.com/crystal-construct/shar/cli/flag"
	"github.com/crystal-construct/shar/model"
	"github.com/spf13/cobra"
)

// rootCmd represents the base command when called without any subcommands
var Cmd = &cobra.Command{
	Use:   "start",
	Short: "Starts a new workflow instance",
	Long:  ``,
	RunE:  run,
	// Uncomment the following line if your bare application
	// has an action associated with it:
}

func run(cmd *cobra.Command, args []string) error {
	ctx := context.Background()
	shar := api.New(api.Logger, flag.Value.Server)
	if err := shar.Dial(); err != nil {
		return fmt.Errorf("error dialling server: %w", err)
	}
	wfiid, err := shar.LaunchWorkflow(ctx, &model.LaunchWorkflowRequest{
		Id:   args[0],
		Vars: nil,
	})
	if err != nil {
		return fmt.Errorf("workflow launch failed: %w", err)
	}
	fmt.Println("workflow instance started. instance-id:", wfiid.Value)
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
