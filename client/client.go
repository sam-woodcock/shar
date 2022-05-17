package client

import (
	"bytes"
	"context"
	"encoding/gob"
	"fmt"
	"github.com/crystal-construct/shar/client/parser"
	"github.com/crystal-construct/shar/client/services"
	"github.com/crystal-construct/shar/model"
	"github.com/crystal-construct/shar/telemetry"
	"github.com/crystal-construct/shar/telemetry/ctxutil"
	"github.com/nats-io/nats.go"
	"github.com/uptrace/opentelemetry-go-extra/otelzap"
	tracesdk "go.opentelemetry.io/otel/sdk/trace"
	"go.uber.org/zap"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/protobuf/proto"
	"os"
	"time"
)

type serviceFn func(ctx context.Context, vars model.Vars) (model.Vars, error)

type Client struct {
	js       nats.JetStreamContext
	SvcTasks map[string]serviceFn
	storage  services.Storage
	svr      model.SharClient
	log      *otelzap.Logger
	tp       *tracesdk.TracerProvider
	address  string
	exporter tracesdk.SpanExporter
}

const serviceName = "shar"

func New(storage services.Storage, log *otelzap.Logger, address string, exporter tracesdk.SpanExporter) *Client {

	return &Client{
		SvcTasks: make(map[string]serviceFn),
		storage:  storage,
		log:      log,
		address:  address,
		exporter: exporter,
	}
}

func (c *Client) Dial(options ...grpc.DialOption) error {
	if tp, err := telemetry.RegisterOpenTelemetry(c.exporter, serviceName); err != nil {
		return err
	} else {
		c.tp = tp
	}

	if len(options) == 0 {
		options = []grpc.DialOption{grpc.WithBlock(), grpc.WithTransportCredentials(insecure.NewCredentials())}
	}
	conn, err := grpc.Dial(c.address, options...)
	if err != nil {
		return fmt.Errorf("failed to connect to grpc: %w", err)
	}
	c.svr = model.NewSharClient(conn)
	return nil
}
func (c *Client) RegisterServiceTask(taskName string, fn serviceFn) {
	if _, ok := c.SvcTasks[taskName]; ok {
		c.log.Fatal("Service task " + taskName + " already registered")
	}
	c.SvcTasks[taskName] = fn
}

func (c *Client) Listen(ctx context.Context) error {
	n, err := nats.Connect(nats.DefaultURL)
	if err != nil {
		return fmt.Errorf("failed to connect to nats: %w", err)
	}
	js, err := n.JetStream()
	if err != nil {
		return fmt.Errorf("failed to connect to jetstream: %w", err)
	}
	c.js = js

	if err := c.listenUserTasks(ctx); err != nil {
		return fmt.Errorf("failed to listen for user tasks: %w", err)
	}
	return nil
}

func (c *Client) listenUserTasks(ctx context.Context) error {
	return c.listen(ctx, "UserTaskExecute", func(msg *nats.Msg) error {
		ut := &model.Job{}
		if err := proto.Unmarshal(msg.Data, ut); err != nil {
			return fmt.Errorf("failed to unmarshal: %w", err)
		}
		if err := c.CompleteUserTask(ctx, ut.Id, model.Vars{}); err != nil {
			return fmt.Errorf("failed to complete user task: %w", err)
		}
		return nil
	})
}

func (c *Client) listen(ctx context.Context, taskType string, fn func(msg *nats.Msg) error) error {
	if _, err := c.js.AddConsumer("WORKFLOW", &nats.ConsumerConfig{
		Durable:       "JobExecuteConsumer",
		Description:   "",
		FilterSubject: "WORKFLOW.Job.Execute.*",
		AckPolicy:     nats.AckExplicitPolicy,
	}); err != nil {
		return fmt.Errorf("failed to add consumer: %w", err)
	}
	sub, err := c.js.PullSubscribe("WORKFLOW.Job.Execute.*", "JobExecuteConsumer")
	if err != nil {
		return fmt.Errorf("failed to pull subscribe: %w", err)
	}
	go func() {
		for {
			var msg *nats.Msg
			if msgs, err := sub.Fetch(1, nats.MaxWait(5*time.Minute)); err != nil {
				fmt.Println(err)
				continue
			} else {
				msg = msgs[0]
			}
			xctx := ctxutil.LoadContextFromNATSHeader(ctx, msg)
			ut := &model.Job{}
			if err := proto.Unmarshal(msg.Data, ut); err != nil {
				fmt.Println(err)
			}
			switch ut.JobType {
			case "ServiceTask":
				job, err := c.storage.GetJob(xctx, ut.Id)
				if err != nil {
					c.log.Ctx(xctx).Error("failed to get job", zap.Error(err), zap.String("JobId", ut.Id))
					continue
				}
				if svcFn, ok := c.SvcTasks[job.Execute]; !ok {
					if err := msg.Ack(); err != nil {
						c.log.Ctx(xctx).Error("failed to find service function", zap.Error(err), zap.String("fn", job.Execute))
					}
				} else {
					newVars, err := svcFn(xctx, c.decodeVars(job.Vars))
					if err != nil {
						if err := msg.Nak(); err != nil {
							c.log.Ctx(xctx).Warn("nak failed", zap.Error(err), zap.String("subject", msg.Subject))
						}
						fmt.Println(err)
						continue
					}
					//TODO: Blend Vars
					if err := c.CompleteServiceTask(xctx, ut.Id, newVars); err != nil {
						if err := msg.Nak(); err != nil {
							c.log.Ctx(xctx).Warn("nak failed", zap.Error(err), zap.String("subject", msg.Subject))
						}
						fmt.Println(err)
						continue
					}
					if err := msg.Ack(); err != nil {
						c.log.Ctx(xctx).Error("service task ack failed", zap.Error(err), zap.String("subject", msg.Subject))
					}
				}
			default:
				if err := msg.Ack(); err != nil {
					c.log.Ctx(xctx).Error("default ack failed", zap.Error(err), zap.String("subject", msg.Subject))
				}
			}
		}
	}()
	return nil
}

func (c *Client) encodeVars(vars map[string]interface{}) []byte {
	var buf bytes.Buffer
	enc := gob.NewEncoder(&buf)
	if err := enc.Encode(vars); err != nil {
		fmt.Println("failed to encode vars", zap.Any("vars", vars))
	}
	return buf.Bytes()
}

func (c *Client) decodeVars(vars []byte) model.Vars {
	ret := make(map[string]interface{})
	r := bytes.NewReader(vars)
	d := gob.NewDecoder(r)
	if err := d.Decode(&ret); err != nil {
		fmt.Println(err)
	}
	return ret
}

func (c *Client) CompleteUserTask(ctx context.Context, jobId string, newVars model.Vars) error {
	job := &model.Job{Id: jobId, Vars: c.encodeVars(newVars)}
	b, err := proto.Marshal(job)
	if err != nil {
		return fmt.Errorf("failed to marshal completed user task: %w", err)
	}
	msg := nats.NewMsg("WORKFLOW.Job.Complete.UserTask")
	msg.Data = b
	ctxutil.LoadNATSHeaderFromContext(ctx, msg)
	_, err = c.js.PublishMsg(msg)
	if err != nil {
		return fmt.Errorf("failed to publish complete user task: %w", err)
	}
	return nil
}

func (c *Client) CompleteServiceTask(ctx context.Context, jobId string, newVars model.Vars) error {
	job := &model.Job{Id: jobId, Vars: c.encodeVars(newVars)}
	b, err := proto.Marshal(job)
	if err != nil {
		return fmt.Errorf("failed to marshal complete service task: %w", err)
	}
	msg := nats.NewMsg("WORKFLOW.Job.Complete.ServiceTask")
	msg.Data = b
	ctxutil.LoadNATSHeaderFromContext(ctx, msg)
	_, err = c.js.PublishMsg(msg)
	if err != nil {
		return fmt.Errorf("failed to publish complete service task: %w", err)
	}
	return nil
}

func (c *Client) CompleteManualTask(ctx context.Context, jobId string, newVars model.Vars) error {
	job := &model.Job{Id: jobId, Vars: c.encodeVars(newVars)}
	b, err := proto.Marshal(job)
	if err != nil {
		return fmt.Errorf("failed to marshal complete manual task: %w", err)
	}
	msg := nats.NewMsg("WORKFLOW.Job.Complete.ManualTask")
	msg.Data = b
	ctxutil.LoadNATSHeaderFromContext(ctx, msg)
	_, err = c.js.PublishMsg(msg)
	if err != nil {
		return fmt.Errorf("failed to publish complete manual task: %w", err)
	}
	return nil
}

func (c *Client) LoadBPMNWorkflowFromFile(ctx context.Context, filename string) (string, error) {
	b, err := os.ReadFile(filename)
	if err != nil {
		return "", fmt.Errorf("failed to load BPMN workflow: %w", err)
	}
	return c.LoadBMPNWorkflowFromBytes(ctx, b)
}

func (c *Client) LoadBMPNWorkflowFromBytes(ctx context.Context, b []byte) (string, error) {
	rdr := bytes.NewReader(b)
	if wf, err := parser.Parse(rdr); err == nil {
		res, err := c.svr.StoreWorkflow(ctx, wf)
		if err != nil {
			return "", fmt.Errorf("failed to store workflow: %w", err)
		}
		return res.Value, nil
	} else {
		return "", fmt.Errorf("failed to parse workflow: %w", err)
	}

}

func (c *Client) LaunchWorkflow(ctx context.Context, workflowID string, vars model.Vars) (string, error) {
	res, err := c.svr.LaunchWorkflow(ctx, &model.LaunchWorkflowRequest{Id: workflowID, Vars: c.encodeVars(vars)})
	if err != nil {
		return "", fmt.Errorf("failed to launch workflow: %w", err)
	}
	return res.Value, nil
}
