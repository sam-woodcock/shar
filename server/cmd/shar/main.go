package main

import (
	"gitlab.com/shar-workflow/shar/server/config"
	"gitlab.com/shar-workflow/shar/server/server"
	"go.uber.org/zap"
	"log"
)

func main() {
	cfg, err := config.GetEnvironment()
	if err != nil {
		log.Fatal(err)
	}

	l, err := zap.NewDevelopment()
	if err != nil {
		panic(err)
	}
	svr := server.New(l)
	svr.Listen(cfg.NatsURL, cfg.Port)
}
