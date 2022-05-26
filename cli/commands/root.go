/*
Copyright © 2022 NAME HERE <EMAIL ADDRESS>

*/
package commands

import (
	"github.com/crystal-construct/shar/cli/api"
	"github.com/crystal-construct/shar/cli/commands/bpmn"
	"github.com/crystal-construct/shar/cli/commands/instance"
	"github.com/crystal-construct/shar/cli/commands/workflow"
	"github.com/crystal-construct/shar/cli/flag"
	"go.uber.org/zap"
	"os"

	"github.com/spf13/cobra"
)

// rootCmd represents the base command when called without any subcommands
var rootCmd = &cobra.Command{
	Use:   "shar",
	Short: "SHAR command line application",
	Long:  ``,
	// Uncomment the following line if your bare application
	// has an action associated with it:
	// Run: func(cmd *cobra.Command, args []string) { },
}

// Execute adds all child commands to the root command and sets flag appropriately.
// This is called by main.main(). It only needs to happen once to the rootCmd.
func Execute() {
	err := rootCmd.Execute()
	if err != nil {
		os.Exit(1)
	}
}

func init() {
	// Here you will define your flag and configuration settings.
	// Cobra supports persistent flag, which, if defined here,
	// will be global for your application.

	// rootCmd.PersistentFlags().StringVar(&cfgFile, "config", "", "config file (default is $HOME/.cli.yaml)")

	// Cobra also supports local flag, which will only run
	// when this action is called directly.
	rootCmd.AddCommand(bpmn.Cmd)
	rootCmd.AddCommand(instance.Cmd)
	rootCmd.AddCommand(workflow.Cmd)
	//rootCmd.Flags().BoolP("toggle", "t", false, "Help message for toggle")
	rootCmd.PersistentFlags().StringVarP(&flag.Value.Server, flag.Server, flag.ServerShort, "localhost:50000", "sets the address of a SHAR server")
	var err error
	api.Logger, err = zap.NewDevelopment()
	if err != nil {
		panic(err)
	}
}
