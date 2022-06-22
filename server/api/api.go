package api

import (
	"context"
	"github.com/crystal-construct/shar/model"
	"github.com/crystal-construct/shar/server/health"
	"github.com/crystal-construct/shar/server/messages"
	"github.com/crystal-construct/shar/server/services"
	"github.com/crystal-construct/shar/server/workflow"
	"go.uber.org/zap"
	"google.golang.org/protobuf/types/known/emptypb"
	"google.golang.org/protobuf/types/known/wrapperspb"
	"sync"
)

type SharServer struct {
	log    *zap.Logger
	health *health.Checker
	ns     *services.NatsService
	engine *workflow.Engine
}

func New(log *zap.Logger, ns *services.NatsService) (*SharServer, error) {
	engine, err := workflow.NewEngine(log, ns)
	if err != nil {
		return nil, err
	}
	if err := engine.Start(context.Background()); err != nil {
		panic(err)
	}
	return &SharServer{
		log:    log,
		ns:     ns,
		engine: engine,
	}, nil
}

func (s *SharServer) storeWorkflow(ctx context.Context, wf *model.Workflow) (*wrapperspb.StringValue, error) {
	res, err := s.engine.LoadWorkflow(ctx, wf)
	return &wrapperspb.StringValue{Value: res}, err
}
func (s *SharServer) launchWorkflow(ctx context.Context, req *model.LaunchWorkflowRequest) (*wrapperspb.StringValue, error) {
	res, err := s.engine.Launch(ctx, req.Name, req.Vars)
	return &wrapperspb.StringValue{Value: res}, err
}

func (s *SharServer) cancelWorkflowInstance(ctx context.Context, req *model.CancelWorkflowInstanceRequest) (*emptypb.Empty, error) {
	err := s.engine.CancelWorkflowInstance(ctx, req.Id)
	if err != nil {
		return nil, err
	}
	return &emptypb.Empty{}, err
}

func (s *SharServer) listWorkflowInstance(ctx context.Context, req *model.ListWorkflowInstanceRequest) (*model.ListWorkflowInstanceResponse, error) {
	wch, errs := s.ns.ListWorkflowInstance(req.WorkflowName)
	ret := make([]*model.ListWorkflowInstanceResult, 0)
	var done bool
	for !done {
		select {
		case winf := <-wch:
			if winf == nil {
				return &model.ListWorkflowInstanceResponse{Result: ret}, nil
			}
			ret = append(ret, &model.ListWorkflowInstanceResult{
				Id:      winf.Id,
				Version: winf.Version,
			})
		case err := <-errs:
			return nil, err
		}
	}
	return &model.ListWorkflowInstanceResponse{Result: ret}, nil
}
func (s *SharServer) getWorkflowInstanceStatus(ctx context.Context, req *model.GetWorkflowInstanceStatusRequest) (*model.WorkflowInstanceStatus, error) {
	res, err := s.ns.GetWorkflowInstanceStatus(req.Id)
	if err != nil {
		return nil, err
	}
	return res, nil
}

func (s *SharServer) listWorkflows(ctx context.Context, p *emptypb.Empty) (*model.ListWorkflowsResponse, error) {
	res, errs := s.ns.ListWorkflows()
	ret := make([]*model.ListWorkflowResult, 0)
	var done bool
	for !done {
		select {
		case winf := <-res:
			if winf == nil {
				done = true
				break
			}
			ret = append(ret, &model.ListWorkflowResult{
				Name:    winf.Name,
				Version: winf.Version,
			})
		case err := <-errs:
			return nil, err
		}
	}
	return &model.ListWorkflowsResponse{Result: ret}, nil
}

func (s *SharServer) sendMessage(ctx context.Context, req *model.SendMessageRequest) (*emptypb.Empty, error) {
	if err := s.ns.PublishMessage(ctx, req.WorkflowInstanceId, req.Name, req.Key); err != nil {
		return nil, err
	}
	return &emptypb.Empty{}, nil
}

var shutdownOnce sync.Once

func (s *SharServer) Shutdown() {
	shutdownOnce.Do(func() {
		s.engine.Shutdown()
		s.log.Info("shar api listener stopped")
	})
}

func (s *SharServer) Listen() error {
	con := s.ns.Conn()
	log := s.log
	_, err := services.Listen(con, log, messages.ApiStoreWorkflow, &model.Workflow{}, s.storeWorkflow)
	if err != nil {
		return err
	}
	_, err = services.Listen(con, log, messages.ApiCancelWorkflowInstance, &model.CancelWorkflowInstanceRequest{}, s.cancelWorkflowInstance)
	if err != nil {
		return err
	}
	_, err = services.Listen(con, log, messages.ApiLaunchWorkflow, &model.LaunchWorkflowRequest{}, s.launchWorkflow)
	if err != nil {
		return err
	}
	_, err = services.Listen(con, log, messages.ApiListWorkflows, &emptypb.Empty{}, s.listWorkflows)
	if err != nil {
		return err
	}
	_, err = services.Listen(con, log, messages.ApiGetWorkflowStatus, &model.GetWorkflowInstanceStatusRequest{}, s.getWorkflowInstanceStatus)
	if err != nil {
		return err
	}
	_, err = services.Listen(con, log, messages.ApiListWorkflowInstance, &model.ListWorkflowInstanceRequest{}, s.listWorkflowInstance)
	if err != nil {
		return err
	}
	_, err = services.Listen(con, log, messages.ApiSendMessage, &model.SendMessageRequest{}, s.sendMessage)
	if err != nil {
		return err
	}
	s.log.Info("shar api listener started")
	return nil
}
