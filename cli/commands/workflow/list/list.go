/*
Copyright © 2022 NAME HERE <EMAIL ADDRESS>

*/
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

// rootCmd represents the base command when called without any subcommands
var Cmd = &cobra.Command{
	Use:   "list",
	Short: "Lists available workflows",
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

func init() {
	// Here you will define your flag and configuration settings.
	// Cobra supports persistent flag, which, if defined here,
	// will be global for your application.

	// rootCmd.PersistentFlags().StringVar(&cfgFile, "config", "", "config file (default is $HOME/.cli.yaml)")

	// Cobra also supports local flag, which will only run
	// when this action is called directly.
}
