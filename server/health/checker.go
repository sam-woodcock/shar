package health

import (
	"context"
	grpcHealth "google.golang.org/grpc/health/grpc_health_v1"
	"sync"
)

type Checker struct {
	grpcHealth.UnimplementedHealthServer
	status grpcHealth.HealthCheckResponse_ServingStatus
	mx     sync.Mutex
}

func New() *Checker {
	return &Checker{
		status: grpcHealth.HealthCheckResponse_NOT_SERVING,
	}
}

func (c *Checker) SetStatus(status grpcHealth.HealthCheckResponse_ServingStatus) {
	c.mx.Lock()
	defer c.mx.Unlock()
	c.status = status
}

func (c *Checker) Check(context.Context, *grpcHealth.HealthCheckRequest) (*grpcHealth.HealthCheckResponse, error) {
	c.mx.Lock()
	defer c.mx.Unlock()
	return &grpcHealth.HealthCheckResponse{
		Status: c.status,
	}, nil
}
