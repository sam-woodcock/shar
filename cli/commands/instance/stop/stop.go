/*
Copyright © 2022 NAME HERE <EMAIL ADDRESS>

*/
package stop

import (
	"github.com/pkg/errors"
	"github.com/spf13/cobra"
)

// rootCmd represents the base command when called without any subcommands
var Cmd = &cobra.Command{
	Use:   "stop",
	Short: "Stop a running workflow instance",
	Long:  ``,
	// Uncomment the following line if your bare application
	// has an action associated with it:
	RunE: run,
}

func run(cmd *cobra.Command, args []string) error {
	return errors.New("Not implemented")
}

func init() {
	// Here you will define your flag and configuration settings.
	// Cobra supports persistent flag, which, if defined here,
	// will be global for your application.

	// rootCmd.PersistentFlags().StringVar(&cfgFile, "config", "", "config file (default is $HOME/.cli.yaml)")

	// Cobra also supports local flag, which will only run
	// when this action is called directly.
}
