package main

import (
	"gitlab.com/shar-workflow/shar/common/logx"
	"gitlab.com/shar-workflow/shar/server/config"
	"gitlab.com/shar-workflow/shar/server/server"
	"golang.org/x/exp/slog"
	"log"
)

func main() {
	cfg, err := config.GetEnvironment()
	if err != nil {
		log.Fatal(err)
	}
	var lev slog.Level
	var addSource bool
	switch cfg.LogLevel {
	case "debug":
		lev = slog.DebugLevel
		addSource = true
	case "info":
		lev = slog.InfoLevel
	case "warn":
		lev = slog.WarnLevel
	default:
		lev = slog.ErrorLevel
	}
	logx.SetDefault(lev, addSource, "shar")
	if err != nil {
		panic(err)
	}
	svr := server.New()
	svr.Listen(cfg.NatsURL, cfg.Port)
}
