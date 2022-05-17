package grpc

import (
	"context"
	"fmt"
	"github.com/crystal-construct/shar/model"
	"github.com/crystal-construct/shar/server/api"
	"github.com/crystal-construct/shar/server/health"
	"github.com/crystal-construct/shar/server/services"
	"github.com/crystal-construct/shar/telemetry"
	grpcMiddleware "github.com/grpc-ecosystem/go-grpc-middleware"
	grpcRecovery "github.com/grpc-ecosystem/go-grpc-middleware/recovery"
	"github.com/nats-io/nats.go"
	"github.com/pkg/errors"
	"github.com/uptrace/opentelemetry-go-extra/otelzap"
	"go.opentelemetry.io/contrib/instrumentation/google.golang.org/grpc/otelgrpc"
	traceSdk "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/trace"
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

const serviceName = "shar"

type Server struct {
	sig            chan os.Signal
	healthService  *health.Checker
	log            *otelzap.Logger
	grpcServer     *gogrpc.Server
	tracerProvider *traceSdk.TracerProvider
	exp            traceSdk.SpanExporter
	api            *api.SharServer
}

// NewSharServer creates a new GRPC server and accepts an opentelemetry exporter.
// Leave the exporter nil if telemetry is not required
func NewSharServer(exporter traceSdk.SpanExporter) *Server {
	s := &Server{
		exp: exporter,
		sig: make(chan os.Signal, 10),
	}
	signal.Notify(s.sig, syscall.SIGINT, syscall.SIGTERM)
	return s
}

// Listen starts the GRPC server for both serving requests, and thw GRPC health endpoint.
func (s *Server) Listen(natsURL string, grpcPort int) {
	ctx := context.Background()
	if s.exp != nil {
		if tp, err := telemetry.RegisterOpenTelemetry(s.exp, serviceName); err != nil {
			log.Fatal("failed to connect to jaeger for opentelemetry", zap.Error(err))
		} else {
			s.tracerProvider = tp
		}
	}

	zlog, err := zap.NewDevelopment(zap.IncreaseLevel(zapcore.DebugLevel))
	s.log = otelzap.New(zlog, otelzap.WithMinLevel(zapcore.DebugLevel))

	// Capture errors and stop signals
	errs := make(chan error)

	// Capture SIGTERM and SIGINT
	signal.Notify(s.sig, syscall.SIGTERM, syscall.SIGINT)

	lis, err := net.Listen("tcp", fmt.Sprintf(":%d", grpcPort))
	if err != nil {
		log.Fatal("failed to listen", zap.Field{Key: "grpcPort", Type: zapcore.Int64Type, Integer: int64(grpcPort)})
	}

	strmInt := make([]gogrpc.StreamServerInterceptor, 0, 3)
	//	grpcRecovery.StreamServerInterceptor(),
	//}
	unaryInt := make([]gogrpc.UnaryServerInterceptor, 0, 3)

	if s.tracerProvider != nil {
		strmInt = append(strmInt, otelgrpc.StreamServerInterceptor(otelgrpc.WithTracerProvider(s.tracerProvider)))
		unaryInt = append(unaryInt, otelgrpc.UnaryServerInterceptor(otelgrpc.WithTracerProvider(s.tracerProvider)))
	}

	unaryInt = append(unaryInt, grpcRecovery.UnaryServerInterceptor())
	strmInt = append(strmInt, grpcRecovery.StreamServerInterceptor())
	
	// Create grpc
	s.grpcServer = gogrpc.NewServer(
		gogrpc.StreamInterceptor(grpcMiddleware.ChainStreamServer(strmInt...)),
		gogrpc.UnaryInterceptor(grpcMiddleware.ChainUnaryServer(unaryInt...)),
	)

	store, queue := s.createServices(natsURL, s.log)

	s.api, s.healthService, err = registerServer(s.log, s.grpcServer, store, queue)
	if err != nil {
		log.Fatal("failed to register grpc", zap.Field{Key: "grpcPort", Type: zapcore.Int64Type, Integer: int64(grpcPort)}, zap.Error(err))
	}

	// Start Server
	go func() {
		if err := s.grpcServer.Serve(lis); err != nil {
			errs <- err
		}
		close(errs)
	}()

	s.healthService.SetStatus(grpcHealth.HealthCheckResponse_SERVING)
	s.log.Info("shar grpc started")

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
	if s.tracerProvider != nil {
		if err := s.tracerProvider.Shutdown(ctx); err != nil {
			panic(err)
		}
	}
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

	var tr trace.Tracer
	if s.tracerProvider != nil {
		s.tracerProvider.Tracer(serviceName)
	}

	queue, err := services.NewNatsQueue(log, conn, nats.FileStorage, tr, 4)
	if err != nil {
		log.Fatal("failed to create NATS queue", zap.Error(err))
	}
	return store, queue
}

func registerServer(log *otelzap.Logger, s *gogrpc.Server, store services.Storage, queue services.Queue) (*api.SharServer, *health.Checker, error) {
	healthService := health.New()
	healthService.SetStatus(grpcHealth.HealthCheckResponse_NOT_SERVING)
	a, err := api.New(log, store, queue)
	if err != nil {
		return nil, nil, errors.Wrap(err, "couldn't register grpc")
	}
	grpcHealth.RegisterHealthServer(s, healthService)
	model.RegisterSharServer(s, a)
	return a, healthService, nil
}
