package integration_tests

import (
	"fmt"
	"github.com/nats-io/nats-server/v2/server"
	sharsvr "gitlab.com/shar-workflow/shar/server/server"
	"go.uber.org/zap"
	"os"
	"testing"
	"time"
)

var testNatsServer *server.Server
var testSharServer *sharsvr.Server

func TestMain(m *testing.M) {
	setup()
	code := m.Run()
	teardown()
	os.Exit(code)
}

func setup() {
	var err error
	testNatsServer, err = server.NewServer(&server.Options{
		ServerName:   "TestNatsServer",
		Host:         "127.0.0.1",
		Port:         4459,
		Trace:        false,
		Debug:        true,
		TraceVerbose: false,
		NoLog:        false,
		JetStream:    true,
	})
	if err != nil {
		panic(err)
	}
	go testNatsServer.Start()
	if !testNatsServer.ReadyForConnections(5 * time.Second) {
		panic("could not start NATS")
	}
	fmt.Println("NATS started")
	l, err := zap.NewDevelopment()
	if err != nil {
		panic(err)
	}
	testSharServer = sharsvr.New(l)
	go testSharServer.Listen("nats://127.0.0.1:4459", 55000)
	for {
		if testSharServer.Ready() {
			break
		}
		fmt.Println("waiting for shar")
		time.Sleep(500 * time.Millisecond)
	}
	fmt.Printf("\033[1;36m%s\033[0m", "> Setup completed\n")
}

func teardown() {
	testSharServer.Shutdown()
	testNatsServer.Shutdown()
	fmt.Println("NATS shut down")
	fmt.Printf("\033[1;36m%s\033[0m", "> Teardown completed")
	fmt.Printf("\n")
}
