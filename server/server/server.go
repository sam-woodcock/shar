package server

import (
	"context"
	"errors"
	"fmt"
	"github.com/nats-io/nats.go"
	"gitlab.com/shar-workflow/shar/common/authn"
	"gitlab.com/shar-workflow/shar/common/authz"
	"gitlab.com/shar-workflow/shar/model"
	"gitlab.com/shar-workflow/shar/server/api"
	"gitlab.com/shar-workflow/shar/server/health"
	"gitlab.com/shar-workflow/shar/server/services"
	"golang.org/x/exp/slog"
	gogrpc "google.golang.org/grpc"
	grpcHealth "google.golang.org/grpc/health/grpc_health_v1"
	"net"
	"os"
	"os/signal"
	"syscall"
)

// Server is the shar server type responsible for hosting the SHAR API.
type Server struct {
	sig                     chan os.Signal
	healthServiceEnabled    bool
	healthService           *health.Checker
	grpcServer              *gogrpc.Server
	api                     *api.SharServer
	ephemeralStorage        bool
	panicRecovery           bool
	allowOrphanServiceTasks bool
	concurrency             int
	apiAuthorizer           authz.APIFunc
	apiAuthenticator        authn.Check
}

// New creates a new SHAR server.
// Leave the exporter nil if telemetry is not required
func New(options ...Option) *Server {
	s := &Server{
		sig:                     make(chan os.Signal, 10),
		healthService:           health.New(),
		panicRecovery:           true,
		allowOrphanServiceTasks: true,
		healthServiceEnabled:    true,
		concurrency:             6,
	}
	for _, i := range options {
		i.configure(s)
	}
	if s.apiAuthorizer == nil {
		slog.Warn("No AuthZ set")
		s.apiAuthorizer = noopAuthZ
	}
	if s.apiAuthenticator == nil {
		slog.Warn("No AuthN set")
		s.apiAuthenticator = noopAuthN
	}
	return s
}

func noopAuthN(ctx context.Context, request *model.ApiAuthenticationRequest) (*model.ApiAuthenticationResponse, error) {
	return &model.ApiAuthenticationResponse{
		User:          "anonymous",
		Authenticated: true,
	}, nil
}

func noopAuthZ(ctx context.Context, request *model.ApiAuthorizationRequest) (*model.ApiAuthorizationResponse, error) {
	return &model.ApiAuthorizationResponse{
		Authorized: true,
	}, nil
}

// Listen starts the GRPC server for both serving requests, and the GRPC health endpoint.
func (s *Server) Listen(natsURL string, grpcPort int) {
	// Capture errors and cancel signals
	errs := make(chan error)

	// Capture SIGTERM and SIGINT
	signal.Notify(s.sig, syscall.SIGTERM, syscall.SIGINT)

	if s.healthServiceEnabled {
		// Create health server and expose on GRPC
		lis, err := net.Listen("tcp", fmt.Sprintf(":%d", grpcPort))
		if err != nil {
			slog.Error("failed to listen", err, slog.Int64("grpcPort", int64(grpcPort)))
			panic(err)
		}

		s.grpcServer = gogrpc.NewServer()
		if err := registerServer(s.grpcServer, s.healthService); err != nil {
			slog.Error("failed to register grpc health server", err, slog.Int64("grpcPort", int64(grpcPort)))
			panic(err)
		}

		// Start health server
		go func() {
			if err := s.grpcServer.Serve(lis); err != nil {
				errs <- err
			}
			close(errs)
		}()
		slog.Info("shar grpc health started")
	} else {
		// Create private health server
		s.healthService.SetStatus(grpcHealth.HealthCheckResponse_NOT_SERVING)
	}

	ns := s.createServices(natsURL, s.ephemeralStorage, s.allowOrphanServiceTasks)
	api, err := api.New(ns, s.panicRecovery, s.apiAuthorizer, s.apiAuthenticator)
	if err != nil {
		panic(err)
	}
	s.api = api
	s.healthService.SetStatus(grpcHealth.HealthCheckResponse_SERVING)

	if err := s.api.Listen(); err != nil {
		panic(err)
	}
	// Log or exit
	select {
	case err := <-errs:
		if err != nil {
			slog.Error("fatal error", err)
			panic("fatal error")
		}
	case <-s.sig:
		s.Shutdown()
	}
}

// Shutdown gracefully shuts down the GRPC server, and requests that
func (s *Server) Shutdown() {

	s.healthService.SetStatus(grpcHealth.HealthCheckResponse_NOT_SERVING)

	s.api.Shutdown()
	if s.healthServiceEnabled {
		s.grpcServer.GracefulStop()
		slog.Info("shar grpc health stopped")
	}
}

func (s *Server) createServices(natsURL string, ephemeral bool, allowOrphanServiceTasks bool) *services.NatsService {
	conn, err := nats.Connect(natsURL)
	if err != nil {
		slog.Error("could not connect to NATS", err, slog.String("url", natsURL))
		panic(err)
	}
	txConn, err := nats.Connect(natsURL)
	if err != nil {
		slog.Error("could not connect to NATS", err, slog.String("url", natsURL))
		panic(err)
	}

	if js, err := conn.JetStream(); err != nil {
		panic(errors.New("cannot form JetSteram connection"))
	} else {
		if _, err := js.AccountInfo(); err != nil {
			panic(errors.New("could not contact JetStream. ensure it is enabled on the specified NATS instance"))
		}
	}

	var store = nats.FileStorage
	if ephemeral {
		store = nats.MemoryStorage
	}
	ns, err := services.NewNatsService(conn, txConn, store, s.concurrency, allowOrphanServiceTasks)
	if err != nil {
		slog.Error("failed to create NATS KV store", err)
		panic(err)
	}
	return ns
}

// Ready returns true if the SHAR server is servicing API calls.
func (s *Server) Ready() bool {
	if s.healthService != nil {
		return s.healthService.GetStatus() == grpcHealth.HealthCheckResponse_SERVING
	} else {
		return false
	}
}

func registerServer(s *gogrpc.Server, hs *health.Checker) error {
	hs.SetStatus(grpcHealth.HealthCheckResponse_NOT_SERVING)
	grpcHealth.RegisterHealthServer(s, hs)
	return nil
}
