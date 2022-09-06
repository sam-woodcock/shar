package server

import (
	"errors"
	"fmt"
	"github.com/nats-io/nats.go"
	"gitlab.com/shar-workflow/shar/server/api"
	"gitlab.com/shar-workflow/shar/server/health"
	"gitlab.com/shar-workflow/shar/server/services"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	gogrpc "google.golang.org/grpc"
	grpcHealth "google.golang.org/grpc/health/grpc_health_v1"
	"log"
	"net"
	"os"
	"os/signal"
	"syscall"
)

type Server struct {
	sig              chan os.Signal
	healthService    *health.Checker
	log              *zap.Logger
	grpcServer       *gogrpc.Server
	api              *api.SharServer
	ephemeralStorage bool
	panicRecovery    bool
}

type Option interface {
	configure(server *Server)
}

func EphemeralStorage() ephemeralStorageOption {
	return ephemeralStorageOption{}
}

type ephemeralStorageOption struct {
	Option
}

func (o ephemeralStorageOption) configure(server *Server) {
	server.ephemeralStorage = true
}

func PanicRecovery(on bool) panicOption {
	return panicOption{value: on}
}

type panicOption struct {
	Option
	value bool
}

func (o panicOption) configure(server *Server) {
	server.panicRecovery = o.value
}

// New creates a new SHAR server.
// Leave the exporter nil if telemetry is not required
func New(log *zap.Logger, options ...Option) *Server {
	s := &Server{
		sig:           make(chan os.Signal, 10),
		log:           log,
		healthService: health.New(),
		panicRecovery: true,
	}
	for _, i := range options {
		i.configure(s)
	}

	return s
}

// Listen starts the GRPC server for both serving requests, and thw GRPC health endpoint.
func (s *Server) Listen(natsURL string, grpcPort int) {
	// Capture errors and cancel signals
	errs := make(chan error)

	// Capture SIGTERM and SIGINT
	signal.Notify(s.sig, syscall.SIGTERM, syscall.SIGINT)

	// Create health server
	lis, err := net.Listen("tcp", fmt.Sprintf(":%d", grpcPort))
	if err != nil {
		log.Fatal("failed to listen", zap.Field{Key: "grpcPort", Type: zapcore.Int64Type, Integer: int64(grpcPort)}, zap.Error(err))
	}

	s.grpcServer = gogrpc.NewServer()
	if err := registerServer(s.grpcServer, s.healthService); err != nil {
		log.Fatal("failed to register grpc health server", zap.Field{Key: "grpcPort", Type: zapcore.Int64Type, Integer: int64(grpcPort)}, zap.Error(err))
	}

	// Start health server
	go func() {
		if err := s.grpcServer.Serve(lis); err != nil {
			errs <- err
		}
		close(errs)
	}()
	s.log.Info("shar grpc health started")

	ns := s.createServices(natsURL, s.log, s.ephemeralStorage)
	s.api, err = api.New(s.log, ns, s.panicRecovery)
	if err != nil {
		panic(err)
	}
	s.healthService.SetStatus(grpcHealth.HealthCheckResponse_SERVING)
	if err := s.api.Listen(); err != nil {
		panic(err)
	}
	// Log or exit
	select {
	case err := <-errs:
		if err != nil {
			log.Fatal("fatal error", zap.Error(err))
		}
	case <-s.sig:
		s.Shutdown()
	}
}

// Shutdown gracefully shuts down the GRPC server, and requests that
func (s *Server) Shutdown() {

	s.healthService.SetStatus(grpcHealth.HealthCheckResponse_NOT_SERVING)
	s.api.Shutdown()
	s.grpcServer.GracefulStop()
	s.log.Info("shar grpc health stopped")

}

func (s *Server) createServices(natsURL string, log *zap.Logger, ephemeral bool) *services.NatsService {
	conn, err := nats.Connect(natsURL)
	if err != nil {
		log.Fatal("could not connect to NATS", zap.Error(err), zap.String("url", natsURL))
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
	ns, err := services.NewNatsService(log, conn, store, 6)
	if err != nil {
		log.Fatal("failed to create NATS KV store", zap.Error(err))
	}
	return ns
}

func (s *Server) Ready() bool {
	return s.healthService.GetStatus() == grpcHealth.HealthCheckResponse_SERVING
}

func registerServer(s *gogrpc.Server, hs *health.Checker) error {
	hs.SetStatus(grpcHealth.HealthCheckResponse_NOT_SERVING)
	grpcHealth.RegisterHealthServer(s, hs)
	return nil
}
