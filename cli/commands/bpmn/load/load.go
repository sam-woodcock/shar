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
