package server

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"github.com/nats-io/nats.go"
	"gitlab.com/shar-workflow/shar/common"
	"gitlab.com/shar-workflow/shar/model"
	"gitlab.com/shar-workflow/shar/server/errors/keys"
	"gitlab.com/shar-workflow/shar/server/messages"
	"gitlab.com/shar-workflow/shar/server/vars"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/exporters/jaeger"
	"go.opentelemetry.io/otel/sdk/instrumentation"
	"go.opentelemetry.io/otel/sdk/resource"
	tracesdk "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/trace"
	"go.uber.org/zap"
	"google.golang.org/protobuf/proto"
	"time"
)

type Server struct {
	log    *zap.Logger
	js     nats.JetStreamContext
	spanKV nats.KeyValue
	res    *resource.Resource
	exp    *jaeger.Exporter
	wfi    nats.KeyValue
}

func New(js nats.JetStreamContext, logger *zap.Logger, res *resource.Resource, exp *jaeger.Exporter) *Server {
	return &Server{
		js:  js,
		log: logger,
		res: res,
		exp: exp,
	}
}

func (s *Server) Listen() error {
	ctx := context.Background()
	closer := make(chan struct{})

	kv, err := s.js.KeyValue(messages.KvTrace)
	if err != nil {
		return err
	}
	s.spanKV = kv
	kv, err = s.js.KeyValue(messages.KvInstance)
	if err != nil {
		return err
	}
	s.wfi = kv
	common.Process(ctx, s.js, s.log, closer, messages.WorkflowStateAll, "Tracing", 1, s.workflowTrace)
	return nil
}

var empty8 = [8]byte{}

func (s *Server) workflowTrace(ctx context.Context, msg *nats.Msg) (bool, error) {

	state := model.WorkflowState{}
	err := proto.Unmarshal(msg.Data, &state)
	if err != nil {
		s.log.Error("unable to unmarshal span", zap.Error(err))
		return true, Abandon(err)
	}

	tid := common.KSuidTo64bit(state.Id)

	if bytes.Equal(tid[:], empty8[:]) {
		return true, nil
	}

	switch msg.Subject {
	case messages.WorkflowInstanceExecute:
		if err := s.saveSpan(ctx, "Workflow Execute", &state, &state); err != nil {
			return false, nil
		}
	case messages.WorkflowTraversalExecute:
	case messages.WorkflowActivityExecute:
		if err := s.spanStart(ctx, &state); err != nil {
			return false, nil
		}
	case messages.WorkflowJobServiceTaskExecute, messages.WorkflowJobUserTaskExecute, messages.WorkflowJobManualTaskExecute:
		if err := s.spanStart(ctx, &state); err != nil {
			return false, nil
		}
	case messages.WorkflowTraversalComplete:
	case messages.WorkflowActivityComplete:
		if err := s.spanEnd(ctx, "Activity: "+state.ElementId, &state); err != nil {
			var escape *AbandonOpError
			if errors.As(err, &escape) {
				s.log.Error("saving Activity.Complete operation abandoned", zap.Error(err),
					zap.String(keys.WorkflowInstanceID, state.WorkflowInstanceId),
					zap.String(keys.TrackingID, state.Id),
					zap.String(keys.ParentTrackingID, state.ParentId),
				)
				return true, err
			}
			return false, nil
		}
	case messages.WorkflowJobServiceTaskComplete, messages.WorkflowJobUserTaskComplete, messages.WorkflowJobManualTaskComplete:
		if err := s.spanEnd(ctx, "Job: "+state.ElementType, &state); err != nil {
			var escape *AbandonOpError
			if errors.As(err, &escape) {
				s.log.Error("saving Job.Complete operation abandoned", zap.Error(err),
					zap.String(keys.WorkflowInstanceID, state.WorkflowInstanceId),
					zap.String(keys.TrackingID, state.Id),
					zap.String(keys.ParentTrackingID, state.ParentId),
				)
				return true, err
			}
			return false, nil
		}
	case messages.WorkflowInstanceComplete:
	case messages.WorkflowInstanceTerminated:
	}
	return true, nil
}

func (s *Server) spanStart(ctx context.Context, state *model.WorkflowState) error {
	err := common.SaveObj(ctx, s.spanKV, state.Id, state)
	if err != nil {
		return err
	}
	return nil
}

func (s *Server) spanEnd(ctx context.Context, name string, state *model.WorkflowState) error {
	oldState := model.WorkflowState{}
	if err := common.LoadObj(s.spanKV, state.Id, &oldState); err != nil {
		s.log.Error("Failed to load span state:", zap.Error(err), zap.String(keys.TrackingID, state.Id))
		return Abandon(err)
	}
	state.WorkflowInstanceId = oldState.WorkflowInstanceId
	state.ParentId = oldState.ParentId
	state.WorkflowId = oldState.WorkflowId
	state.ElementId = oldState.ElementId
	state.Execute = oldState.Execute
	state.Condition = oldState.Condition
	state.ElementType = oldState.ElementType
	state.State = oldState.State
	if err := s.saveSpan(ctx, name, &oldState, state); err != nil {
		s.log.Error("Failed to record span:", zap.Error(err), zap.String(keys.TrackingID, state.Id))
		return err
	}
	return nil
}

func (s *Server) saveSpan(ctx context.Context, name string, oldState *model.WorkflowState, newState *model.WorkflowState) error {
	traceID := common.KSuidTo128bit(oldState.WorkflowInstanceId)
	spanID := common.KSuidTo64bit(oldState.Id)
	parentID := common.KSuidTo64bit(oldState.ParentId)
	parentSpan := trace.SpanContext{}
	if len(oldState.ParentId) > 0 {
		parentSpan = trace.NewSpanContext(trace.SpanContextConfig{
			TraceID: traceID,
			SpanID:  parentID,
		})
	}

	at := map[string]*string{
		keys.ElementID:          &oldState.ElementId,
		keys.ElementType:        &oldState.ElementType,
		keys.WorkflowID:         &oldState.WorkflowId,
		keys.WorkflowInstanceID: &oldState.WorkflowInstanceId,
		keys.Condition:          oldState.Condition,
		keys.Execute:            oldState.Execute,
		keys.State:              &oldState.State,
		"trackingId":            &oldState.Id,
		"parentTrId":            &oldState.ParentId,
	}

	kv, err := vars.Decode(s.log, newState.Vars)
	if err != nil {
		return Abandon(err)
	}

	for k, v := range kv {
		val := fmt.Sprintf("var.%+v", v)
		at[k] = &val
	}
	attrs := buildAttrs(at)
	err = s.exp.ExportSpans(
		ctx,
		[]tracesdk.ReadOnlySpan{
			&sharSpan{
				ReadOnlySpan: nil,
				SpanName:     name,
				SpanCtx: trace.NewSpanContext(trace.SpanContextConfig{
					TraceID:    traceID,
					SpanID:     spanID,
					TraceFlags: 0,
					TraceState: trace.TraceState{},
					Remote:     false,
				}),
				SpanParent: parentSpan,
				Kind:       0,
				Start:      time.Unix(0, oldState.UnixTimeNano),
				End:        time.Unix(0, newState.UnixTimeNano),
				Attrs:      attrs,
				SpanLinks:  []tracesdk.Link{},
				SpanEvents: []tracesdk.Event{},
				SpanStatus: tracesdk.Status{
					Code: codes.Ok,
				},
				InstrumentationLib: instrumentation.Library{
					Name:    "TEST",
					Version: "0.1",
				},
				SpanResource: s.res,
				ChildCount:   0,
			},
		},
	)
	if err != nil {
		return err
	}
	err = s.spanKV.Delete(oldState.Id)
	if err != nil {
		s.log.Warn("Could not delete the cached span", zap.Error(err), zap.String(keys.TrackingID, oldState.Id))
	}
	return nil
}

func buildAttrs(m map[string]*string) []attribute.KeyValue {
	ret := make([]attribute.KeyValue, 0, len(m))
	for k, v := range m {
		if v != nil && *v != "" {
			ret = append(ret, attribute.String(k, *v))
		}
	}
	return ret
}
