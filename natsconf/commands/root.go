package commands

import (
	"github.com/nats-io/nats.go"
	"github.com/spf13/cobra"
	"gitlab.com/shar-workflow/shar/natsconf/flag"
	"gitlab.com/shar-workflow/shar/natsconf/output"
	"go.uber.org/zap"
	"os"
)

// rootCmd represents the base command when called without any subcommands
var rootCmd = &cobra.Command{
	Use:   "natsconf",
	Short: "natsconf command line application",
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
	//rootCmd.AddCommand(bpmn.Cmd)

	rootCmd.PersistentFlags().StringVarP(&flag.Value.Server, flag.Server, flag.ServerShort, nats.DefaultURL, "sets the address of a NATS server")
	rootCmd.PersistentFlags().StringVarP(&flag.Value.LogLevel, flag.LogLevel, flag.LogLevelShort, "error", "sets the logging level for the CLI")
	var err error
	lev, err := zap.ParseAtomicLevel(flag.Value.LogLevel)
	if err != nil {
		panic("could not parse log level")
	}
	output.Logger, err = zap.Config{
		Level:            lev,
		Development:      true,
		Encoding:         "console",
		EncoderConfig:    zap.NewDevelopmentEncoderConfig(),
		OutputPaths:      []string{"stderr"},
		ErrorOutputPaths: []string{"stderr"},
	}.Build()
	if err != nil {
		panic(err)
	}
}
