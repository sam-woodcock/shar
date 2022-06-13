package services

import (
	"context"
	"errors"
	"fmt"
	"github.com/crystal-construct/shar/internal/messages"
	"github.com/crystal-construct/shar/model"
	"github.com/crystal-construct/shar/telemetry/ctxutil"
	"github.com/nats-io/nats.go"
	"go.opentelemetry.io/otel/trace"
	"go.uber.org/zap"
	"google.golang.org/grpc/codes"
	"google.golang.org/protobuf/proto"
	"time"
)

type NatsQueue struct {
	js                        nats.JetStreamContext
	con                       *nats.Conn
	eventProcessor            EventProcessorFunc
	eventJobCompleteProcessor CompleteJobProcessorFunc
	log                       *zap.Logger
	storageType               nats.StorageType
	concurrency               int
	tracer                    trace.Tracer
	closing                   chan struct{}
	workflowTracking          string
}

func NewNatsQueue(log *zap.Logger, conn *nats.Conn, storageType nats.StorageType, concurrency int) (*NatsQueue, error) {
	if concurrency < 1 || concurrency > 200 {
		return nil, errors.New("invalid concurrency set")
	}
	js, err := conn.JetStream()
	if err != nil {
		return nil, err
	}
	return &NatsQueue{
		concurrency:      concurrency,
		storageType:      storageType,
		con:              conn,
		js:               js,
		log:              log,
		closing:          make(chan struct{}),
		workflowTracking: "WORKFLOW_TRACKING",
	}, nil
}

func (q *NatsQueue) StartProcessing(ctx context.Context) error {
	scfg := &nats.StreamConfig{
		Name:      "WORKFLOW",
		Subjects:  messages.AllMessages,
		Storage:   q.storageType,
		Retention: nats.InterestPolicy,
	}

	ccfg := &nats.ConsumerConfig{
		Durable:         "Traversal",
		Description:     "Traversal processing queue",
		AckPolicy:       nats.AckExplicitPolicy,
		FilterSubject:   messages.WorkflowTraversalExecute,
		MaxRequestBatch: 1,
	}

	tcfg := &nats.ConsumerConfig{
		Durable:         "Tracking",
		Description:     "Tracking queue for sequential processing",
		AckPolicy:       nats.AckExplicitPolicy,
		FilterSubject:   "WORKFLOW.>",
		MaxAckPending:   1,
		MaxRequestBatch: 1,
	}

	acfg := &nats.ConsumerConfig{
		Durable:         "API",
		Description:     "Api queue",
		AckPolicy:       nats.AckExplicitPolicy,
		FilterSubject:   messages.ApiAll,
		MaxRequestBatch: 1,
	}

	if _, err := q.js.StreamInfo(scfg.Name); err == nats.ErrStreamNotFound {
		if _, err := q.js.AddStream(scfg); err != nil {
			panic(err)
		}
	} else if err != nil {
		panic(err)
	}

	if _, err := q.js.ConsumerInfo(scfg.Name, ccfg.Durable); err == nats.ErrConsumerNotFound {
		if _, err := q.js.AddConsumer("WORKFLOW", ccfg); err != nil {
			panic(err)
		}
	} else if err != nil {
		panic(err)
	}

	if _, err := q.js.ConsumerInfo(scfg.Name, tcfg.Durable); err == nats.ErrConsumerNotFound {
		if _, err := q.js.AddConsumer("WORKFLOW", tcfg); err != nil {
			panic(err)
		}
	} else if err != nil {
		panic(err)
	}

	if _, err := q.js.ConsumerInfo(scfg.Name, acfg.Durable); err == nats.ErrConsumerNotFound {
		if _, err := q.js.AddConsumer("WORKFLOW", acfg); err != nil {
			panic(err)
		}
	} else if err != nil {
		panic(err)
	}

	go func() {
		q.processTraversals(ctx)
	}()

	go func() {
		q.processTracking(ctx)
	}()

	go q.processCompletedJobs(ctx)
	return nil
}
func (q *NatsQueue) SetEventProcessor(processor EventProcessorFunc) {
	q.eventProcessor = processor
}
func (q *NatsQueue) SetCompleteJobProcessor(processor CompleteJobProcessorFunc) {
	q.eventJobCompleteProcessor = processor
}

func (q *NatsQueue) PublishJob(ctx context.Context, stateName string, el *model.Element, job *model.WorkflowState) error {
	return q.PublishWorkflowState(ctx, stateName, job)
}

func (q *NatsQueue) PublishWorkflowState(ctx context.Context, stateName string, state *model.WorkflowState) error {
	state.UnixTimeNano = time.Now().UnixNano()
	msg := nats.NewMsg(stateName)
	if b, err := proto.Marshal(state); err != nil {
		return err
	} else {
		msg.Data = b
	}
	ctxutil.LoadNATSHeaderFromContext(ctx, msg)
	if _, err := q.js.PublishMsg(msg); err != nil {
		return err
	}
	return nil
}

func (q *NatsQueue) processTraversals(ctx context.Context) {
	q.process(ctx, messages.WorkflowTraversalExecute, "Traversal", func(ctx context.Context, msg *nats.Msg) error {
		var traversal model.WorkflowState
		if err := proto.Unmarshal(msg.Data, &traversal); err != nil {
			return fmt.Errorf("could not unmarshal traversal proto: %w", err)
		}
		if q.eventProcessor != nil {
			if err := q.eventProcessor(ctx, traversal.WorkflowInstanceId, traversal.ElementId, traversal.TrackingId, traversal.Vars); err != nil {
				return fmt.Errorf("could not process event: %w", err)
			}
		}
		return nil
	})
	return
}

func (q *NatsQueue) processTracking(ctx context.Context) {
	q.process(ctx, "WORKFLOW.>", "Tracking", q.track)
	return
}

func (q *NatsQueue) processCompletedJobs(ctx context.Context) {
	q.process(ctx, messages.WorkFlowJobCompleteAll, "JobCompleteConsumer", func(ctx context.Context, msg *nats.Msg) error {
		var job model.WorkflowState
		if err := proto.Unmarshal(msg.Data, &job); err != nil {
			return err
		}
		if q.eventJobCompleteProcessor != nil {
			if err := q.eventJobCompleteProcessor(ctx, job.TrackingId, job.Vars); err != nil {
				return err
			}
		}
		return nil
	})
}

func (q *NatsQueue) process(ctx context.Context, subject string, durable string, fn func(ctx context.Context, msg *nats.Msg) error) {
	for i := 0; i < q.concurrency; i++ {
		go func() {
			sub, err := q.js.PullSubscribe(subject, durable)
			if err != nil {
				q.log.Error("process pull subscribe error", zap.Error(err), zap.String("subject", subject))
				return
			}
			for {
				select {
				case <-q.closing:
					return
				default:
				}
				ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
				msg, err := sub.Fetch(1, nats.Context(ctx))
				if err != nil {
					if err == context.DeadlineExceeded {
						cancel()
						continue
					}
					// Log Error
					q.log.Error("message fetch error", zap.Error(err))
					cancel()
					return
				}
				executeCtx := ctxutil.LoadContextFromNATSHeader(ctx, msg[0])
				err = fn(executeCtx, msg[0])
				if err != nil {
					q.log.Error("processing error", zap.Error(err))
				}
				if err := msg[0].Ack(); err != nil {
					q.log.Error("processing failed to ack", zap.Error(err))
				}

				cancel()
			}
		}()
	}
}

func (q *NatsQueue) track(ctx context.Context, msg *nats.Msg) error {
	kv, err := q.js.KeyValue(q.workflowTracking)
	if err != nil {
		return err
	}
	switch msg.Subject {
	case messages.WorkflowInstanceExecute,
		messages.WorkflowTraversalExecute,
		messages.WorkflowActivityExecute,
		messages.WorkflowJobServiceTaskExecute,
		messages.WorkflowJobManualTaskExecute,
		messages.WorkflowJobUserTaskExecute:
		st := &model.WorkflowState{}
		if err := proto.Unmarshal(msg.Data, st); err != nil {
			return err
		}
		if err := SaveObj(kv, st.WorkflowInstanceId, st); err != nil {
			return err
		}
	case messages.WorkflowInstanceComplete:
		st := &model.WorkflowState{}
		if err := proto.Unmarshal(msg.Data, st); err != nil {
			return err
		}
		if err := kv.Delete(st.WorkflowInstanceId); err != nil {
			return err
		}
	}
	return nil
}

func (q *NatsQueue) Conn() *nats.Conn {
	return q.con
}

func Listen[T proto.Message, U proto.Message](con *nats.Conn, log *zap.Logger, subject string, req T, fn func(ctx context.Context, req T) (U, error)) (*nats.Subscription, error) {
	sub, err := con.Subscribe(subject, func(msg *nats.Msg) {
		ctx := context.Background()
		if err := callApi(ctx, req, msg, fn); err != nil {
			log.Error("registering listener for "+subject+" failed", zap.Error(err))
		}
	})
	if err != nil {
		return nil, err
	}
	return sub, nil
}

func callApi[T proto.Message, U proto.Message](ctx context.Context, container T, msg *nats.Msg, fn func(ctx context.Context, req T) (U, error)) error {
	defer recoverAPIpanic(msg)
	if err := proto.Unmarshal(msg.Data, container); err != nil {
		errorResponse(msg, codes.Internal, err.Error())
		return err
	}
	resMsg, err := fn(ctx, container)
	if err != nil {
		errorResponse(msg, codes.Internal, err.Error())
		return err
	}
	res, err := proto.Marshal(resMsg)
	if err != nil {
		errorResponse(msg, codes.Internal, err.Error())
		return err
	}
	if err := msg.Respond(res); err != nil {
		errorResponse(msg, codes.Internal, err.Error())
		return err
	}
	return nil
}

func recoverAPIpanic(msg *nats.Msg) {
	if r := recover(); r != nil {
		errorResponse(msg, codes.Internal, r)
		fmt.Println("recovered from ", r)
	}
}

func errorResponse(m *nats.Msg, code codes.Code, msg any) {
	if err := m.Respond(apiError(codes.Internal, msg)); err != nil {
		fmt.Println("failed to send error response: " + string(apiError(codes.Internal, msg)))
	}
}

func apiError(code codes.Code, msg any) []byte {
	return []byte(fmt.Sprintf("ERR_%d|%+v", code, msg))
}
