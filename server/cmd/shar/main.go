package main

import (
	"github.com/crystal-construct/shar/server/config"
	"github.com/crystal-construct/shar/server/server"
	"go.uber.org/zap"
	"log"
)

func main() {
	cfg, err := config.GetEnvironment()
	if err != nil {
		log.Fatal(err)
	}

	log, err := zap.NewDevelopment()
	if err != nil {
		panic(err)
	}
	svr := server.New(log)
	svr.Listen(cfg.NatsURL, cfg.Port)
}
