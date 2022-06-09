package main

import (
	"github.com/crystal-construct/shar/server/config"
	"github.com/crystal-construct/shar/server/server"
	"log"
)

func main() {
	cfg, err := config.GetEnvironment()
	if err != nil {
		log.Fatal(err)
	}

	svr := server.New()
	svr.Listen(cfg.NatsURL, cfg.Port)
}
