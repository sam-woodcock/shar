/*
Copyright © 2022 NAME HERE <EMAIL ADDRESS>

*/
package status

import (
	"errors"
	"github.com/spf13/cobra"
)

// rootCmd represents the base command when called without any subcommands
var Cmd = &cobra.Command{
	Use:   "status",
	Short: "Gets the status of a running workflow instance",
	Long:  ``,
	RunE:  run,
	// Uncomment the following line if your bare application
	// has an action associated with it:
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
