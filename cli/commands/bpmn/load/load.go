/*
Copyright © 2022 NAME HERE <EMAIL ADDRESS>

*/
package load

import (
	"bytes"
	"context"
	"fmt"
	"github.com/crystal-construct/shar/cli/api"
	"github.com/crystal-construct/shar/cli/flag"
	"github.com/crystal-construct/shar/client/parser"
	"github.com/pkg/errors"
	"github.com/spf13/cobra"
	"os"
)

var Cmd = &cobra.Command{
	Use:   "load",
	Short: "Loads a BPMN XML file into shar",
	Long:  ``,
	RunE:  run,
}

func init() {
	// Here you will define your flag and configuration settings.
	// Cobra supports persistent flag, which, if defined here,
	// will be global for your application.

	// rootCmd.PersistentFlags().StringVar(&cfgFile, "config", "", "config file (default is $HOME/.cli.yaml)")

	// Cobra also supports local flag, which will only run
	// when this action is called directly.
}

func run(cmd *cobra.Command, args []string) error {
	ctx := context.Background()
	if len(args) == 0 {
		return errors.New("no file provided")
	}
	b, err := os.ReadFile(args[0])
	if err != nil {
		return fmt.Errorf("error reading file: %w", err)
	}
	shar := api.New(api.Logger, flag.Value.Server)
	if err := shar.Dial(); err != nil {
		return fmt.Errorf("error dialling server: %w", err)
	}
	buf := bytes.NewBuffer(b)
	wf, err := parser.Parse(buf)
	iid, err := shar.StoreWorkflow(ctx, wf)
	if err != nil {
		return fmt.Errorf("workflow could not be loaded: %w", err)
	}
	fmt.Println("workflow ("+iid.Value+") loaded. workflow-id:", wf.Name)
	return nil
}
