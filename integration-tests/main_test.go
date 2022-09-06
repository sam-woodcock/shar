package main

import (
	"context"
	"fmt"
	"github.com/nats-io/nats-server/v2/server"
	"github.com/nats-io/nats.go"
	"github.com/stretchr/testify/require"
	sharsvr "gitlab.com/shar-workflow/shar/server/server"
	"go.uber.org/zap"
	"testing"
	"time"
)

const natsHost = "127.0.0.1"
const natsPort = 4459

var natsURL = fmt.Sprintf("nats://%s:%v", natsHost, natsPort)

type integration struct {
	testNatsServer *server.Server
	testSharServer *sharsvr.Server
	finalVars      map[string]interface{}
	test           *testing.T
}

/*
	func TestMain(m *testing.M) {
		//	setup()
		code := m.Run()
		//	teardown()
		os.Exit(code)
	}
*/

//goland:noinspection GoNilness
func (s *integration) setup(t *testing.T) {
	s.test = t
	s.finalVars = make(map[string]interface{})
	logger, err := zap.NewDevelopment()
	if err != nil {
		panic(err)
	}
	nl := &NatsLogger{l: logger}

	s.testNatsServer, err = server.NewServer(&server.Options{
		ConfigFile:            "",
		ServerName:            "TestNatsServer",
		Host:                  natsHost,
		Port:                  natsPort,
		ClientAdvertise:       "",
		Trace:                 false,
		Debug:                 false,
		TraceVerbose:          false,
		NoLog:                 false,
		NoSigs:                false,
		NoSublistCache:        false,
		NoHeaderSupport:       false,
		DisableShortFirstPing: false,
		Logtime:               false,
		MaxConn:               0,
		MaxSubs:               0,
		MaxSubTokens:          0,
		Nkeys:                 nil,
		Users:                 nil,
		Accounts: []*server.Account{
			{
				Name:   "sysacc",
				Nkey:   "",
				Issuer: "",
			},
		},
		NoAuthUser:                 "",
		SystemAccount:              "sysacc",
		NoSystemAccount:            true,
		Username:                   "",
		Password:                   "",
		Authorization:              "",
		PingInterval:               0,
		MaxPingsOut:                0,
		HTTPHost:                   "",
		HTTPPort:                   0,
		HTTPBasePath:               "",
		HTTPSPort:                  0,
		AuthTimeout:                0,
		MaxControlLine:             0,
		MaxPayload:                 0,
		MaxPending:                 0,
		Cluster:                    server.ClusterOpts{},
		Gateway:                    server.GatewayOpts{},
		LeafNode:                   server.LeafNodeOpts{},
		JetStream:                  true,
		JetStreamMaxMemory:         0,
		JetStreamMaxStore:          0,
		JetStreamDomain:            "",
		JetStreamExtHint:           "",
		JetStreamKey:               "",
		JetStreamUniqueTag:         "",
		JetStreamLimits:            server.JSLimitOpts{},
		StoreDir:                   "",
		JsAccDefaultDomain:         nil,
		Websocket:                  server.WebsocketOpts{},
		MQTT:                       server.MQTTOpts{},
		ProfPort:                   0,
		PidFile:                    "",
		PortsFileDir:               "",
		LogFile:                    "",
		LogSizeLimit:               0,
		Syslog:                     false,
		RemoteSyslog:               "",
		Routes:                     nil,
		RoutesStr:                  "",
		TLSTimeout:                 0,
		TLS:                        false,
		TLSVerify:                  false,
		TLSMap:                     false,
		TLSCert:                    "",
		TLSKey:                     "",
		TLSCaCert:                  "",
		TLSConfig:                  nil,
		TLSPinnedCerts:             nil,
		TLSRateLimit:               0,
		AllowNonTLS:                false,
		WriteDeadline:              0,
		MaxClosedClients:           0,
		LameDuckDuration:           0,
		LameDuckGracePeriod:        0,
		MaxTracedMsgLen:            0,
		TrustedKeys:                nil,
		TrustedOperators:           nil,
		AccountResolver:            nil,
		AccountResolverTLSConfig:   nil,
		AlwaysEnableNonce:          false,
		CustomClientAuthentication: nil,
		CustomRouterAuthentication: nil,
		CheckConfig:                false,
		ConnectErrorReports:        0,
		ReconnectErrorReports:      0,
		Tags:                       nil,
		OCSPConfig:                 nil,
	})
	s.testNatsServer.SetLogger(nl, false, false)
	if err != nil {
		panic(err)
	}
	go s.testNatsServer.Start()
	if !s.testNatsServer.ReadyForConnections(5 * time.Second) {
		panic("could not start NATS")
	}
	s.test.Log("NATS started")

	l, err := zap.NewProduction()
	if err != nil {
		panic(err)
	}
	s.testSharServer = sharsvr.New(l, sharsvr.EphemeralStorage())
	go s.testSharServer.Listen(natsURL, 55000)
	for {
		if s.testSharServer.Ready() {
			break
		}
		fmt.Println("waiting for shar")
		time.Sleep(500 * time.Millisecond)
	}
	s.test.Logf("\033[1;36m%s\033[0m", "> Setup completed\n")
}

func (s *integration) teardown() {
	n, err := s.GetJetstream()
	require.NoError(s.test, err)
	sub, err := n.PullSubscribe("WORKFLOW.>", "fin")
	require.NoError(s.test, err)
	for {
		ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
		_, err := sub.Fetch(1, nats.Context(ctx))
		if err == context.DeadlineExceeded {
			cancel()
			break
		}
		if err != nil {
			cancel()
			s.test.Fatal(err)
		}
		//fmt.Println("MSG: ", *msg[0])
		cancel()
	}
	s.test.Log("TEARDOWN")
	s.testSharServer.Shutdown()
	s.testNatsServer.Shutdown()
	s.testNatsServer.WaitForShutdown()
	s.test.Log("NATS shut down")
	s.test.Logf("\033[1;36m%s\033[0m", "> Teardown completed")
	s.test.Log("\n")
}

func (s *integration) GetJetstream() (nats.JetStreamContext, error) { //nolint:ireturn
	con, err := s.GetNats()
	if err != nil {
		return nil, err
	}
	js, err := con.JetStream()
	if err != nil {
		return nil, err
	}
	return js, nil
}

func (s *integration) GetNats() (*nats.Conn, error) {
	con, err := nats.Connect(natsURL)
	if err != nil {
		return nil, err
	}
	return con, nil
}

type NatsLogger struct {
	l *zap.Logger
}

// Log a notice statement
func (n *NatsLogger) Noticef(format string, v ...interface{}) {
	n.l.Info(fmt.Sprintf(format, v...))
}

// Log a warning statement
func (n *NatsLogger) Warnf(format string, v ...interface{}) {
	n.l.Warn(fmt.Sprintf(format, v...))
}

// Log a fatal error
func (n *NatsLogger) Fatalf(format string, v ...interface{}) {
	n.l.Fatal(fmt.Sprintf(format, v...))
}

// Log an error
func (n *NatsLogger) Errorf(format string, v ...interface{}) {
	n.l.Error(fmt.Sprintf(format, v...))
}

// Log a debug statement
func (n *NatsLogger) Debugf(format string, v ...interface{}) {
	n.l.Debug(fmt.Sprintf(format, v...))
}

// Log a trace statement
func (n *NatsLogger) Tracef(format string, v ...interface{}) {
	n.l.Info("trace: " + fmt.Sprintf(format, v...))
}
