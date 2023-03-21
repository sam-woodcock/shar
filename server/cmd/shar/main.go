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
		lev = slog.LevelDebug
		addSource = true
	case "info":
		lev = slog.LevelInfo
	case "warn":
		lev = slog.LevelWarn
	default:
		lev = slog.LevelError
	}
	logx.SetDefault(lev, addSource, "shar")
	if err != nil {
		panic(err)
	}
	svr := server.New(server.Concurrency(cfg.Concurrency))
	svr.Listen(cfg.NatsURL, cfg.Port)
}
