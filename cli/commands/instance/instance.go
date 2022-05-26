/*
Copyright © 2022 NAME HERE <EMAIL ADDRESS>

*/
package instance

import (
	"github.com/crystal-construct/shar/cli/commands/instance/list"
	"github.com/crystal-construct/shar/cli/commands/instance/status"
	"github.com/crystal-construct/shar/cli/commands/instance/stop"
	"github.com/spf13/cobra"
)

// rootCmd represents the base command when called without any subcommands
var Cmd = &cobra.Command{
	Use:   "instance",
	Short: "Commands for dealing with workflow instances",
	Long:  ``,
	// Uncomment the following line if your bare application
	// has an action associated with it:
}

func init() {
	// Here you will define your flag and configuration settings.
	// Cobra supports persistent flag, which, if defined here,
	// will be global for your application.

	// rootCmd.PersistentFlags().StringVar(&cfgFile, "config", "", "config file (default is $HOME/.cli.yaml)")

	// Cobra also supports local flag, which will only run
	// when this action is called directly.
	Cmd.AddCommand(list.Cmd)
	Cmd.AddCommand(status.Cmd)
	Cmd.AddCommand(stop.Cmd)
}
