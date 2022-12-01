package server

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"github.com/nats-io/nats.go"
	"gitlab.com/shar-workflow/shar/common"
	"gitlab.com/shar-workflow/shar/common/subj"
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
	"golang.org/x/exp/slog"
	"google.golang.org/protobuf/proto"
	"strings"
	"time"
)

// Server is the shar server type responsible for hosting the telemetry server.
type Server struct {
	js     nats.JetStreamContext
	spanKV nats.KeyValue
	res    *resource.Resource
	exp    *jaeger.Exporter
	wfi    nats.KeyValue
}

// New creates a new telemetry server.
func New(ctx context.Context, js nats.JetStreamContext, res *resource.Resource, exp *jaeger.Exporter) *Server {
	return &Server{
		js:  js,
		res: res,
		exp: exp,
	}
}

// Listen starts the telemtry server.
func (s *Server) Listen() error {
	ctx := context.Background()
	closer := make(chan struct{})

	kv, err := s.js.KeyValue(messages.KvTracking)
	if err != nil {
		return fmt.Errorf("listen failed to attach to tracking key value database: %w", err)
	}
	s.spanKV = kv
	kv, err = s.js.KeyValue(messages.KvInstance)
	if err != nil {
		return fmt.Errorf("listen failed to attach to instance key value database: %w", err)
	}
	s.wfi = kv
	err = common.Process(ctx, s.js, "telemetry", closer, subj.NS(messages.WorkflowStateAll, "*"), "Tracing", 1, s.workflowTrace)
	if err != nil {
		return fmt.Errorf("listen failed to start telemetry handler: %w", err)
	}
	return nil
}

var empty8 = [8]byte{}

func (s *Server) workflowTrace(ctx context.Context, log *slog.Logger, msg *nats.Msg) (bool, error) {
	state, done, err2 := s.decodeState(ctx, msg)
	if done {
		return done, err2
	}

	switch {
	case strings.HasSuffix(msg.Subject, ".State.Workflow.Execute"):
		if err := s.saveSpan(ctx, "Workflow Execute", state, state); err != nil {
			return false, nil
		}
	case strings.HasSuffix(msg.Subject, ".State.Traversal.Execute"):
	case strings.HasSuffix(msg.Subject, ".State.Activity.Execute"):
		if err := s.spanStart(ctx, state); err != nil {
			return false, nil
		}
	case strings.Contains(msg.Subject, ".State.Job.Execute.ServiceTask"),
		strings.HasSuffix(msg.Subject, ".State.Job.Execute.UserTask"),
		strings.HasSuffix(msg.Subject, ".State.Job.Execute.ManualTask"),
		strings.Contains(msg.Subject, ".State.Job.Execute.SendMessage"):
		if err := s.spanStart(ctx, state); err != nil {
			return false, nil
		}
	case strings.HasSuffix(msg.Subject, ".State.Traversal.Complete"):
	case strings.HasSuffix(msg.Subject, ".State.Activity.Complete"),
		strings.HasSuffix(msg.Subject, ".State.Activity.Abort"):
		if err := s.spanEnd(ctx, "Activity: "+state.ElementId, state); err != nil {
			var escape *AbandonOpError
			if errors.As(err, &escape) {
				log.Error("saving Activity.Complete operation abandoned", err,
					slog.String(keys.WorkflowInstanceID, state.WorkflowInstanceId),
					slog.String(keys.TrackingID, common.TrackingID(state.Id).ID()),
					slog.String(keys.ParentTrackingID, common.TrackingID(state.Id).ParentID()),
				)
				return true, err
			}
			return false, nil
		}
	case strings.Contains(msg.Subject, ".State.Job.Complete.ServiceTask"),
		strings.Contains(msg.Subject, ".State.Job.Abort.ServiceTask"),
		strings.Contains(msg.Subject, ".State.Job.Complete.UserTask"),
		strings.Contains(msg.Subject, ".State.Job.Complete.ManualTask"),
		strings.Contains(msg.Subject, ".State.Job.Complete.SendMessage"):
		if err := s.spanEnd(ctx, "Job: "+state.ElementType, state); err != nil {
			var escape *AbandonOpError
			if errors.As(err, &escape) {
				log.Error("saving Job.Complete operation abandoned", err,
					slog.String(keys.WorkflowInstanceID, state.WorkflowInstanceId),
					slog.String(keys.TrackingID, common.TrackingID(state.Id).ID()),
					slog.String(keys.ParentTrackingID, common.TrackingID(state.Id).ParentID()),
				)
				return true, err
			}
			return false, nil
		}
	case strings.Contains(msg.Subject, ".State.Job.Complete.SendMessage"):
	case strings.Contains(msg.Subject, ".State.Log."):

	//case strings.HasSuffix(msg.Subject, ".State.Workflow.Complete"):
	//case strings.HasSuffix(msg.Subject, ".State.Workflow.Terminated"):
	default:

	}
	return true, nil
}

func (s *Server) decodeState(ctx context.Context, msg *nats.Msg) (*model.WorkflowState, bool, error) {
	log := slog.FromContext(ctx)
	state := &model.WorkflowState{}
	err := proto.Unmarshal(msg.Data, state)
	if err != nil {
		log.Error("unable to unmarshal span", err)
		return &model.WorkflowState{}, true, abandon(err)
	}

	tid := common.KSuidTo64bit(common.TrackingID(state.Id).ID())

	if bytes.Equal(tid[:], empty8[:]) {
		return &model.WorkflowState{}, true, nil
	}
	return state, false, nil
}

func (s *Server) spanStart(ctx context.Context, state *model.WorkflowState) error {
	err := common.SaveObj(ctx, s.spanKV, common.TrackingID(state.Id).ID(), state)
	if err != nil {
		return fmt.Errorf("span-start failed fo save object: %w", err)
	}
	return nil
}

func (s *Server) spanEnd(ctx context.Context, name string, state *model.WorkflowState) error {
	log := slog.FromContext(ctx)
	oldState := model.WorkflowState{}
	if err := common.LoadObj(ctx, s.spanKV, common.TrackingID(state.Id).ID(), &oldState); err != nil {
		log.Error("Failed to load span state:", err, slog.String(keys.TrackingID, common.TrackingID(state.Id).ID()))
		return abandon(err)
	}
	state.WorkflowInstanceId = oldState.WorkflowInstanceId
	state.Id = oldState.Id
	state.WorkflowId = oldState.WorkflowId
	state.ElementId = oldState.ElementId
	state.Execute = oldState.Execute
	state.Condition = oldState.Condition
	state.ElementType = oldState.ElementType
	state.State = oldState.State
	if err := s.saveSpan(ctx, name, &oldState, state); err != nil {
		log.Error("Failed to record span:", err, slog.String(keys.TrackingID, common.TrackingID(state.Id).ID()))
		return fmt.Errorf("save span failed: %w", err)
	}
	return nil
}

func (s *Server) saveSpan(ctx context.Context, name string, oldState *model.WorkflowState, newState *model.WorkflowState) error {
	log := slog.FromContext(ctx)
	traceID := common.KSuidTo128bit(oldState.WorkflowInstanceId)
	spanID := common.KSuidTo64bit(common.TrackingID(oldState.Id).ID())
	parentID := common.KSuidTo64bit(common.TrackingID(oldState.Id).ParentID())
	parentSpan := trace.SpanContext{}
	if len(common.TrackingID(oldState.Id).ParentID()) > 0 {
		parentSpan = trace.NewSpanContext(trace.SpanContextConfig{
			TraceID: traceID,
			SpanID:  parentID,
		})
	}
	pid := common.TrackingID(oldState.Id).ParentID()
	id := common.TrackingID(oldState.Id).ID()
	st := oldState.State.String()
	at := map[string]*string{
		keys.ElementID:          &oldState.ElementId,
		keys.ElementType:        &oldState.ElementType,
		keys.WorkflowID:         &oldState.WorkflowId,
		keys.WorkflowInstanceID: &oldState.WorkflowInstanceId,
		keys.Condition:          oldState.Condition,
		keys.Execute:            oldState.Execute,
		keys.State:              &st,
		"trackingId":            &id,
		"parentTrId":            &pid,
	}

	kv, err := vars.Decode(ctx, newState.Vars)
	if err != nil {
		return abandon(err)
	}

	for k, v := range kv {
		val := fmt.Sprintf("%+v", v)
		at["var."+k] = &val
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
		return fmt.Errorf("export spans failed: %w", err)
	}
	err = s.spanKV.Delete(common.TrackingID(oldState.Id).ID())
	if err != nil {
		id := common.TrackingID(oldState.Id).ID()
		log.Warn("Could not delete the cached span", err, slog.String(keys.TrackingID, id))
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
