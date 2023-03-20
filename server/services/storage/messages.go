package storage

import (
	"context"
	errors2 "errors"
	"fmt"
	"github.com/nats-io/nats.go"
	"github.com/segmentio/ksuid"
	"gitlab.com/shar-workflow/shar/common"
	"gitlab.com/shar-workflow/shar/common/expression"
	"gitlab.com/shar-workflow/shar/common/header"
	"gitlab.com/shar-workflow/shar/common/logx"
	"gitlab.com/shar-workflow/shar/common/subj"
	"gitlab.com/shar-workflow/shar/model"
	"gitlab.com/shar-workflow/shar/server/errors"
	"gitlab.com/shar-workflow/shar/server/messages"
	"gitlab.com/shar-workflow/shar/server/vars"
	"golang.org/x/exp/slog"
	"google.golang.org/protobuf/proto"
	"time"
)

func (s *Nats) messageKick(ctx context.Context) error {
	go func() {
		select {
		case <-s.closing:
			return
		default:

			for {
				time.Sleep(1 * time.Second)
				keys, err := s.wfMessageInterest.Keys()
				if err != nil {
					continue
				}
				for _, k := range keys {
					fmt.Println("kick")
					interest := &model.MessageRecipients{}
					if err := common.LoadObj(ctx, s.wfMessageInterest, k, interest); err != nil {
						continue
					}
					if err := s.deliverMessageToJobRecipient(ctx, interest.Recipient, k); err != nil {
						continue
					}
				}
			}
		}
	}()
	return nil
}

// PublishMessage publishes a workflow message.
func (s *Nats) PublishMessage(ctx context.Context, name string, key string, vars []byte) error {
	sharMsg := &model.MessageInstance{
		Name:           name,
		CorrelationKey: key,
		Vars:           vars,
	}
	msg := nats.NewMsg(fmt.Sprintf(messages.WorkflowMessage, "default"))
	b, err := proto.Marshal(sharMsg)
	if err != nil {
		return fmt.Errorf("marshal message for publishing: %w", err)
	}
	msg.Data = b
	if err := header.FromCtxToMsgHeader(ctx, &msg.Header); err != nil {
		return fmt.Errorf("add header to published workflow state: %w", err)
	}
	pubCtx, cancel := context.WithTimeout(ctx, s.publishTimeout)
	defer cancel()
	id := ksuid.New().String()
	if _, err := s.txJS.PublishMsg(msg, nats.Context(pubCtx), nats.MsgId(id)); err != nil {
		log := logx.FromContext(ctx)
		log.Error("publish message", err, slog.String("nats.msg.id", id), slog.Any("msg", sharMsg), slog.String("subject", msg.Subject))
		return fmt.Errorf("publish message: %w", err)
	}
	return nil
}

func (s *Nats) processMessages(ctx context.Context) error {
	err := common.Process(ctx, s.js, "message", s.closing, subj.NS(messages.WorkflowMessage, "*"), "Message", s.concurrency, s.processMessage)
	if err != nil {
		return fmt.Errorf("start message processor: %w", err)
	}
	return nil
}

func (s *Nats) processMessage(ctx context.Context, log *slog.Logger, msg *nats.Msg) (bool, error) {
	// Unpack the message Instance
	instance := &model.MessageInstance{}
	if err := proto.Unmarshal(msg.Data, instance); err != nil {
		return false, fmt.Errorf("unmarshal message proto: %w", err)
	}
	if msgs, err := s.js.KeyValue(subj.NS("Message_%s_", "default") + instance.Name); err != nil {
		return false, fmt.Errorf("process message getting message keys: %w", err)
	} else {
		if err := common.Save(ctx, msgs, ksuid.New().String(), msg.Data); err != nil {
			return false, fmt.Errorf("process message saving messaGE: %w", err)
		}
	}
	subs := &model.MessageRecipients{}
	if err := common.LoadObj(ctx, s.wfMessageInterest, instance.Name, subs); errors2.Is(err, nats.ErrKeyNotFound) {
		return true, nil
	} else if err != nil {
		return true, err
	}
	s.deliverMessageToJobRecipient(ctx, subs.Recipient, instance.Name)
	return true, nil
}

func (s *Nats) deliverMessageToJobRecipient(ctx context.Context, recipients []*model.MessageRecipient, msgName string) error {
	msgs, err := s.js.KeyValue(subj.NS("Message_%s_", "default") + msgName)
	if err != nil {
		return fmt.Errorf("get message kv: %w", err)
	}
	keys, err := msgs.Keys()
	if err != nil {
		if errors2.Is(err, nats.ErrNoKeysFound) {
			return nil
		}
		return fmt.Errorf("get message keys: %w", err)
	}
	for _, k := range keys {
		m := &model.MessageInstance{}
		if err := common.LoadObj(ctx, msgs, k, m); err != nil {
			return fmt.Errorf("get message instance: %w", err)
		}
		for _, r := range recipients {
			if r.Type == model.RecipientType_job && r.CorrelationKey == m.CorrelationKey {
				if lck, err := common.Lock(s.wfLock, r.Id); err != nil {
					slog.Error("delivery obtaining lock: %w", err)
					continue
				} else if !lck {
					continue
				}
				if err := s.deliverMessageToJob(ctx, r.Id, m); errors2.Is(err, errors.ErrJobNotFound) {
				} else if err != nil {
					slog.Error("delivering message", err)
					panic(err)
					continue
				}
				if err := common.Delete(msgs, k); err != nil {
					slog.Error("delivering message", err)
					panic(err)
				}
				if err := common.UnLock(s.wfLock, r.Id); err != nil {
					return fmt.Errorf("delivery releasing lock: %w", err)
				}
			}
		}
	}
	return nil
}

func (s *Nats) deliverMessageToJob(ctx context.Context, jobID string, instance *model.MessageInstance) error {
	job, err := s.GetJob(ctx, jobID)
	if errors2.Is(err, nats.ErrKeyNotFound) {
		return nil
	} else if err != nil {
		return err
	}
	job.Vars = instance.Vars
	if err := s.PublishWorkflowState(ctx, messages.WorkflowJobAwaitMessageComplete, job); err != nil {
		return fmt.Errorf("publising complete message job: %w", err)
	}

	if err := common.UpdateObj(ctx, s.wfMessageInterest, instance.Name, &model.MessageRecipients{}, func(v *model.MessageRecipients) (*model.MessageRecipients, error) {
		removeWhere(v.Recipient, func(recipient *model.MessageRecipient) bool {
			return recipient.Type == model.RecipientType_job && recipient.Id == jobID
		})
		return v, nil
	}); err != nil {
		return fmt.Errorf("updating message subscriptions: %w", err)
	}
	return nil
}
func (s *Nats) processAwaitMessageExecute(ctx context.Context) error {
	if err := common.Process(ctx, s.js, "gatewayExecute", s.closing, subj.NS(messages.WorkflowJobAwaitMessageExecute, "*"), "AwaitMessageConsumer", s.concurrency, s.awaitMessageProcessor); err != nil {
		return fmt.Errorf("start process launch processor: %w", err)
	}
	return nil
}

// awaitMessageProcessor waits for WORKFLOW.*.State.Job.AwaitMessage.Execute job and executes a delivery
func (s *Nats) awaitMessageProcessor(ctx context.Context, log *slog.Logger, msg *nats.Msg) (bool, error) {
	job := &model.WorkflowState{}
	if err := proto.Unmarshal(msg.Data, job); err != nil {
		return false, fmt.Errorf("unmarshal during process launch: %w", err)
	}
	if _, _, err := s.HasValidProcess(ctx, job.ProcessInstanceId, job.WorkflowInstanceId); errors2.Is(err, errors.ErrWorkflowInstanceNotFound) || errors2.Is(err, errors.ErrProcessInstanceNotFound) {
		log := logx.FromContext(ctx)
		log.Log(ctx, slog.LevelInfo, "processLaunch aborted due to a missing process")
		return true, err
	} else if err != nil {
		return false, err
	}

	el, err := s.GetElement(ctx, job)
	if err != nil {
		return false, fmt.Errorf("get message element: %w", err)
	}

	vrs, err := vars.Decode(ctx, job.Vars)
	if err != nil {
		return false, fmt.Errorf("decoding vars for message correlation: %w", err)
	}
	resAny, err := expression.EvalAny(ctx, "= "+el.Execute, vrs)
	if err != nil {
		return false, fmt.Errorf("evaluating message correlation expression: %w", err)
	}
	res := fmt.Sprintf("%+v", resAny)
	interest := &model.MessageRecipients{}
	if err := common.UpdateObj(ctx, s.wfMessageInterest, el.Msg, interest, func(v *model.MessageRecipients) (*model.MessageRecipients, error) {
		v.Recipient = append(v.Recipient, &model.MessageRecipient{
			Type:           model.RecipientType_job,
			Id:             common.TrackingID(job.Id).ID(),
			CorrelationKey: res,
		})
		return v, nil
	}); err != nil {
		return false, fmt.Errorf("update the workflow message subscriptions during await message: %w", err)
	}
	if err := s.deliverMessageToJobRecipient(ctx, interest.Recipient, el.Msg); err != nil {
		return false, fmt.Errorf("attempting delivery: %w", err)
	}
	return true, nil
}
