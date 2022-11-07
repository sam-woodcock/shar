package commands

import (
	"github.com/nats-io/nats.go"
	"gitlab.com/shar-workflow/shar/zen-shar/flag"
	"gitlab.com/shar-workflow/shar/zen-shar/server"
	"go.uber.org/zap"
	"os"
	"os/signal"
	"syscall"

	"github.com/spf13/cobra"
)

var sig = make(chan os.Signal, 10)

// rootCmd represents the base command when called without any subcommands
var rootCmd = &cobra.Command{
	Use:   "zen-shar",
	Short: "ZEN SHAR development server",
	Long:  ``,
	// Uncomment the following line if your bare application
	// has an action associated with it:
	// Run: func(cmd *cobra.Command, args []string) { },
	RunE: run,
}

func run(cmd *cobra.Command, args []string) error {
	// Capture SIGTERM and SIGINT
	signal.Notify(sig, syscall.SIGTERM, syscall.SIGINT)

	ns, ss, err := server.GetServers("127.0.0.1", 4222, logger)
	if err != nil {
		panic(err)
	}

	<-sig
	defer ss.Shutdown()
	defer ns.Shutdown()
	return nil
}

// Execute adds all child commands to the root command and sets flag appropriately.
// This is called by main.main(). It only needs to happen once to the rootCmd.
func Execute() {
	err := rootCmd.Execute()
	if err != nil {
		os.Exit(1)
	}
}

var logger *zap.Logger

func init() {
	rootCmd.PersistentFlags().StringVarP(&flag.Value.Server, flag.Server, flag.ServerShort, nats.DefaultURL, "sets the address of a NATS server")
	rootCmd.PersistentFlags().StringVarP(&flag.Value.LogLevel, flag.LogLevel, flag.LogLevelShort, "error", "sets the logging level")
	var err error
	lev, err := zap.ParseAtomicLevel(flag.Value.LogLevel)
	if err != nil {
		panic("could not parse log level")
	}
	logger, err = zap.Config{
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
