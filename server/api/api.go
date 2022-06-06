package api

import (
	"context"
	"github.com/crystal-construct/shar/model"
	"github.com/crystal-construct/shar/server/health"
	"github.com/crystal-construct/shar/server/services"
	"github.com/crystal-construct/shar/server/workflow"
	"github.com/golang/protobuf/ptypes/empty"
	"github.com/golang/protobuf/ptypes/wrappers"
	"github.com/uptrace/opentelemetry-go-extra/otelzap"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"sync"
)

type SharServer struct {
	model.SharServer
	log    *otelzap.Logger
	health *health.Checker
	store  services.Storage
	queue  services.Queue
	engine *workflow.Engine
}

func New(log *otelzap.Logger, store services.Storage, queue services.Queue) (*SharServer, error) {
	engine, err := workflow.NewEngine(log, store, queue)
	if err != nil {
		return nil, err
	}
	if err := engine.Start(context.Background()); err != nil {
		panic(err)
	}
	return &SharServer{
		log:    log,
		store:  store,
		queue:  queue,
		engine: engine,
	}, nil
}

func (s *SharServer) StoreWorkflow(ctx context.Context, process *model.Process) (*wrappers.StringValue, error) {
	res, err := s.engine.LoadWorkflow(ctx, process)
	return &wrappers.StringValue{Value: res}, err
}
func (s *SharServer) LaunchWorkflow(ctx context.Context, req *model.LaunchWorkflowRequest) (*wrappers.StringValue, error) {
	res, err := s.engine.Launch(ctx, req.Name, req.Vars)
	return &wrappers.StringValue{Value: res}, err
}

func (s *SharServer) CancelWorkflowInstance(ctx context.Context, req *model.CancelWorkflowInstanceRequest) (*empty.Empty, error) {
	err := s.engine.CancelWorkflowInstance(ctx, req.Id)
	if err != nil {
		return nil, err
	}
	return &empty.Empty{}, err
}

func (s *SharServer) ListWorkflowInstance(req *model.ListWorkflowInstanceRequest, svr model.Shar_ListWorkflowInstanceServer) error {
	wch, errs := s.store.ListWorkflowInstance(req.WorkflowName)
	var done bool
	for !done {
		select {
		case winf := <-wch:
			if winf == nil {
				return nil
			}
			if err := svr.Send(&model.WorkflowInstanceInfo{
				Id:         winf.WorkflowInstanceId,
				WorkflowId: winf.WorkflowId,
			}); err != nil {
				return err
			}
		case err := <-errs:
			return err
		}
	}
	return nil
}
func (s *SharServer) GetWorkflowInstanceStatus(ctx context.Context, req *model.GetWorkflowInstanceStatusRequest) (*model.WorkflowInstanceStatus, error) {
	return nil, status.Errorf(codes.Unimplemented, "method GetWorkflowInstanceStatus not implemented")
}

func (s *SharServer) ListWorkflows(p *empty.Empty, svr model.Shar_ListWorkflowsServer) error {
	res, errs := s.store.ListWorkflows()
	var done bool
	for !done {
		select {
		case winf := <-res:
			if winf == nil {
				return nil
			}
			if err := svr.Send(&model.ListWorkflowResult{
				Name:    winf.Name,
				Version: winf.Version,
			}); err != nil {
				return err
			}
		case err := <-errs:
			return err
		}
	}
	return nil
}

var shutdownOnce sync.Once

func (s *SharServer) Shutdown() {
	shutdownOnce.Do(func() {
		s.engine.Shutdown()
	})
}
