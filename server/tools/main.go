package main

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/nats-io/nats.go"
	"gitlab.com/shar-workflow/shar/client"
	"time"
)

func main() {
	ctx := context.Background()

	// Dial shar
	cl := client.New(client.WithEphemeralStorage())
	err := cl.Dial(ctx, "nats://127.0.0.1:4222")
	if err != nil {
		panic(err)
	}

	go func() {
		for {
			stats, _ := cl.GetServerInstanceStats(ctx)
			fmt.Printf("%+v\n", stats)
			time.Sleep(5 * time.Second)
		}
	}()

	con, _ := nats.Connect("nats://localhost:4459")
	js, _ := con.JetStream()
	i := js.Streams()
	for s := range i {
		fmt.Println(s.Config.Name)
		j, _ := json.Marshal(s.Config)
		fmt.Println(string(j))
	}
	c := js.ConsumerNames("WORKFLOW")
	for s := range c {
		fmt.Println(s)
		j, _ := json.Marshal(s)
		fmt.Println(string(j))
	}
	time.Sleep(2 * time.Minute)
}
