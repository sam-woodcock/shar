package server

import (
	"fmt"
	"github.com/nats-io/nats-server/v2/server"
	sharsvr "gitlab.com/shar-workflow/shar/server/server"
	"golang.org/x/exp/slog"
	"strconv"
	"time"
)

// GetServers returns a test NATS and SHAR server.
func GetServers(natsHost string, natsPort int) (*sharsvr.Server, *server.Server, error) {
	nsvr, err := server.NewServer(&server.Options{
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
	if err != nil {
		return nil, nil, fmt.Errorf("failed to create a new server instance: %w", err)
	}
	nl := &NatsLogger{}
	nsvr.SetLogger(nl, false, false)

	go nsvr.Start()
	if !nsvr.ReadyForConnections(5 * time.Second) {
		panic("could not start NATS")
	}
	slog.Info("NATS started")

	ssvr := sharsvr.New(sharsvr.EphemeralStorage(), sharsvr.PanicRecovery(false))
	go ssvr.Listen(natsHost+":"+strconv.Itoa(natsPort), 55000)
	for {
		if ssvr.Ready() {
			break
		}
		slog.Info("waiting for shar")
		time.Sleep(500 * time.Millisecond)
	}
	slog.Info("Setup completed")
	return ssvr, nsvr, nil
}
