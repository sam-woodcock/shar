package server

import (
	"context"
	"fmt"
	"github.com/crystal-construct/shar/server/api"
	"github.com/crystal-construct/shar/server/health"
	"github.com/crystal-construct/shar/server/services"
	"github.com/nats-io/nats.go"
	"github.com/uptrace/opentelemetry-go-extra/otelzap"
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
	sig           chan os.Signal
	healthService *health.Checker
	log           *otelzap.Logger
	grpcServer    *gogrpc.Server
	api           *api.SharServer
}

// New creates a new SHAR server.
// Leave the exporter nil if telemetry is not required
func New() *Server {
	s := &Server{
		sig: make(chan os.Signal, 10),
	}
	signal.Notify(s.sig, syscall.SIGINT, syscall.SIGTERM)
	return s
}

// Listen starts the GRPC server for both serving requests, and thw GRPC health endpoint.
func (s *Server) Listen(natsURL string, grpcPort int) {
	ctx := context.Background()

	zlog, err := zap.NewDevelopment(zap.IncreaseLevel(zapcore.DebugLevel))
	s.log = otelzap.New(zlog, otelzap.WithMinLevel(zapcore.DebugLevel))

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
	s.healthService, err = registerServer(s.grpcServer)
	if err != nil {
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

	store, queue := s.createServices(natsURL, s.log)
	s.api, err = api.New(s.log, store, queue)
	s.healthService.SetStatus(grpcHealth.HealthCheckResponse_SERVING)
	s.api.Listen()
	// Log or exit
	select {
	case err := <-errs:
		if err != nil {
			log.Fatal("fatal error", zap.Error(err))
		}
	case <-s.sig:
		s.Shutdown(ctx)
	}
}

// Shutdown gracefully shuts down the GRPC server, and requests that
func (s *Server) Shutdown(ctx context.Context) {
	s.healthService.SetStatus(grpcHealth.HealthCheckResponse_NOT_SERVING)
	s.grpcServer.GracefulStop()
	s.api.Shutdown()
	s.log.Info("shar grpc stopped")
}

func (s *Server) createServices(natsURL string, log *otelzap.Logger) (*services.NatsKVStore, *services.NatsQueue) {
	conn, err := nats.Connect(natsURL)
	if err != nil {
		log.Fatal("could not connect to NATS", zap.Error(err), zap.String("url", natsURL))
	}

	store, err := services.NewNatsKVStore(log, conn, nats.FileStorage)
	if err != nil {
		log.Fatal("failed to create NATS KV store", zap.Error(err))
	}

	queue, err := services.NewNatsQueue(log, conn, nats.FileStorage, 4)
	if err != nil {
		log.Fatal("failed to create NATS queue", zap.Error(err))
	}
	return store, queue
}

func registerServer(s *gogrpc.Server) (*health.Checker, error) {
	healthService := health.New()
	healthService.SetStatus(grpcHealth.HealthCheckResponse_NOT_SERVING)
	grpcHealth.RegisterHealthServer(s, healthService)
	return healthService, nil
}
