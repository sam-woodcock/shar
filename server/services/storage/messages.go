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

func (s *Nats) ensureMessageBuckets(ctx context.Context, wf *model.Workflow) error {
	for _, m := range wf.Messages {
		if err := common.Save(ctx, s.wfMsgTypes, m.Name, []byte{}); err != nil {
			return &errors.ErrWorkflowFatal{Err: err}
		}
		if err := common.EnsureBucket(s.js, s.storageType, msgTxBucket(ctx, m.Name), 0); err != nil {
			return &errors.ErrWorkflowFatal{Err: err}
		}
		if err := common.EnsureBucket(s.js, s.storageType, msgRxBucket(ctx, m.Name), 0); err != nil {
			return &errors.ErrWorkflowFatal{Err: err}
		}

		ks := ksuid.New()

		//TODO: this should only happen if there is a task associated with message send
		if err := common.Save(ctx, s.wfClientTask, wf.Name+"_"+m.Name, []byte(ks.String())); err != nil {
			return fmt.Errorf("create a client task during workflow creation: %w", err)
		}

		jxCfg := &nats.ConsumerConfig{
			Durable:       "ServiceTask_" + wf.Name + "_" + m.Name,
			Description:   "",
			FilterSubject: subj.NS(messages.WorkflowJobSendMessageExecute, "default") + "." + wf.Name + "_" + m.Name,
			AckPolicy:     nats.AckExplicitPolicy,
			MaxAckPending: 65536,
		}
		if err := ensureConsumer(s.js, "WORKFLOW", jxCfg); err != nil {
			return fmt.Errorf("add service task consumer: %w", err)
		}
	}
	return nil
}

var messageKickInterval = time.Second * 10

func (s *Nats) messageKick(ctx context.Context) error {
	sub, err := s.js.PullSubscribe(messages.WorkflowMessageKick, "MessageKick")
	if err != nil {
		return fmt.Errorf("creating message kick subscription: %w", err)
	}
	go func() {
		for {
			select {
			case <-s.closing:
				return
			default:
				pctx, cancel := context.WithTimeout(ctx, 5*time.Minute)
				msgs, err := sub.Fetch(1, nats.Context(pctx))
				if err != nil || len(msgs) == 0 {
					slog.Warn("pulling kick message")
					cancel()
					time.Sleep(20 * time.Second)
					continue
				}
				msg := msgs[0]
				msgTypes, err := s.wfMsgTypes.Keys()
				if err != nil {
					goto continueLoop
				}
				for _, k := range msgTypes {
					_, err := s.iterateRxMessages(ctx, k, "", func(k string, recipient *model.MessageRecipient) (bool, error) {
						fmt.Println("dodeliver")
						if delivered, err := s.deliverMessageToJobRecipient(ctx, recipient, k); err != nil {
							return false, err
						} else if !delivered {
							return false, err
						}
						return true, nil
					})
					if err != nil {
						slog.Warn(err.Error())
					}
				}
			continueLoop:
				if err := msg.NakWithDelay(messageKickInterval); err != nil {
					slog.Warn("message nak: " + err.Error())
				}
				cancel()
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
	if msgs, err := s.js.KeyValue(msgTxBucket(ctx, instance.Name)); err != nil {
		return false, fmt.Errorf("process message getting message keys: %w", err)
	} else {
		if err := common.Save(ctx, msgs, ksuid.New().String(), msg.Data); err != nil {
			return false, fmt.Errorf("process message saving message: %w", err)
		}
	}
	_, err := s.iterateRxMessages(ctx, instance.Name, instance.CorrelationKey, func(k string, recipient *model.MessageRecipient) (bool, error) {
		if instance.CorrelationKey == recipient.CorrelationKey {
			if delivered, err := s.deliverMessageToJobRecipient(ctx, recipient, instance.Name); err != nil {
				return false, err
			} else if delivered {
				return true, nil
			}
		}
		return false, nil
	})
	if err != nil {
		slog.Warn("process message delivering message: " + err.Error())
	}
	return true, nil
}

func (s *Nats) deliverMessageToJobRecipient(ctx context.Context, recipient *model.MessageRecipient, msgName string) (bool, error) {
	msgs, err := s.js.KeyValue(msgTxBucket(ctx, msgName))
	if err != nil {
		return false, fmt.Errorf("get message kv: %w", err)
	}
	return s.iterateTxMessages(
		ctx,
		msgName,
		recipient.CorrelationKey,
		func(k string, m *model.MessageInstance) (bool, error) {
			if recipient.Type == model.RecipientType_job {
				if err := s.deliverMessageToJob(ctx, recipient.Id, m); errors2.Is(err, errors.ErrJobNotFound) {
					return false, err
				} else if err != nil {
					return false, err
				}
				if err := common.Delete(msgs, k); err != nil {
					return false, fmt.Errorf("deleting message: %w", err)
				}
			}
			return true, nil
		})
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
	rx, err := s.js.KeyValue(msgRxBucket(ctx, instance.Name))
	if err != nil {
		return fmt.Errorf("obtaining message recipient kv: %w", err)
	}
	if err := common.Delete(rx, jobID); err != nil {
		return fmt.Errorf("updating message subscriptions: %w", err)
	}
	return nil
}
func (s *Nats) processAwaitMessageExecute(ctx context.Context) error {
	if err := common.Process(ctx, s.js, "messageExecute", s.closing, subj.NS(messages.WorkflowJobAwaitMessageExecute, "*"), "AwaitMessageConsumer", s.concurrency, s.awaitMessageProcessor); err != nil {
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
	if errors2.Is(err, nats.ErrKeyNotFound) {
		return true, errors.ErrWorkflowFatal{Err: fmt.Errorf("finding associated element: %w", err)}
	} else if err != nil {
		return false, fmt.Errorf("get message element: %w", err)

	}

	vrs, err := vars.Decode(ctx, job.Vars)
	if err != nil {
		return false, errors.ErrWorkflowFatal{Err: fmt.Errorf("decoding vars for message correlation: %w", err)}
	}
	resAny, err := expression.EvalAny(ctx, "= "+el.Execute, vrs)
	if err != nil {
		return false, errors.ErrWorkflowFatal{Err: fmt.Errorf("evaluating message correlation expression: %w", err)}
	}
	res := fmt.Sprintf("%+v", resAny)
	recipient := &model.MessageRecipient{
		Type:           model.RecipientType_job,
		Id:             common.TrackingID(job.Id).ID(),
		CorrelationKey: res,
	}
	rx, err := s.js.KeyValue(msgRxBucket(ctx, el.Msg))
	if err != nil {
		return false, fmt.Errorf("obtaining message recipient kv: %w", err)
	}
	if err := common.SaveObj(ctx, rx, common.TrackingID(job.Id).ID(), recipient); err != nil {
		return false, fmt.Errorf("update the workflow message subscriptions during await message: %w", err)
	}
	if _, err := s.deliverMessageToJobRecipient(ctx, recipient, el.Msg); err != nil {
		return false, fmt.Errorf("attempting delivery: %w", err)
	}
	return true, nil
}

type correlatable interface {
	proto.Message
	GetCorrelationKey() string
}

func (s *Nats) iterateTxMessages(ctx context.Context, name string, correlationKey string, fn func(k string, m *model.MessageInstance) (bool, error)) (bool, error) {
	tx, err := s.js.KeyValue(msgTxBucket(ctx, name))
	if err != nil {
		return false, fmt.Errorf("opening receive bucket: %w", err)
	}
	return lockedIterator(ctx, s, tx, correlationKey, "message", &model.MessageInstance{}, fn)
}

func (s *Nats) iterateRxMessages(ctx context.Context, name string, correlationKey string, fn func(k string, r *model.MessageRecipient) (bool, error)) (bool, error) {
	rx, err := s.js.KeyValue(msgRxBucket(ctx, name))
	if err != nil {
		return false, fmt.Errorf("opening receive bucket: %w", err)
	}
	return lockedIterator(ctx, s, rx, correlationKey, "recipient", &model.MessageRecipient{}, fn)
}

func lockedIterator[T correlatable](ctx context.Context, s *Nats, rx nats.KeyValue, match string, iteratorType string, iterateValue T, fn func(k string, r T) (bool, error)) (bool, error) {
	keys, err := rx.Keys()
	if err != nil {
		return false, fmt.Errorf("retrieving %s keys: %w", iteratorType, err)
	}
	return lockedIterate(ctx, s, rx, match, iteratorType, iterateValue, fn, keys)
}

func lockedIterate[T correlatable](ctx context.Context, s *Nats, rx nats.KeyValue, match string, iteratorType string, iterateValue T, fn func(k string, r T) (bool, error), keys []string) (bool, error) {
	for _, k := range keys {
		if err := common.LoadObj(ctx, rx, k, iterateValue); err != nil {
			continue
		}
		if len(match) == 0 || iterateValue.GetCorrelationKey() != match {
			continue
		}
		if lck, err := common.Lock(s.wfLock, k); err != nil {
			slog.Error("%s iterator obtaining lock: %w", iteratorType, err)
			if err := common.UnLock(s.wfLock, k); err != nil {
				slog.Warn("unlocking " + iteratorType + ": " + err.Error())
			}
			continue
		} else if !lck {
			if err := common.UnLock(s.wfLock, k); err != nil {
				slog.Warn("unlocking " + iteratorType + ": " + err.Error())
			}
			continue
		}
		if delivered, err := fn(k, iterateValue); err != nil {
			slog.Warn("delivering to " + iteratorType + ": " + err.Error())
			if err := common.UnLock(s.wfLock, k); err != nil {
				slog.Warn("unlocking " + iteratorType + ": " + err.Error())
			}
			continue
		} else if delivered {
			if err := common.UnLock(s.wfLock, k); err != nil {
				slog.Warn("unlocking " + iteratorType + ": " + err.Error())
			}
			return true, nil
		}
		if err := common.UnLock(s.wfLock, k); err != nil {
			slog.Warn("unlocking " + iteratorType + ": " + err.Error())
		}
	}
	return false, nil
}

func msgTxBucket(ctx context.Context, name string) string {
	return subj.NS("MsgTx_%s_", "default") + name
}

func msgRxBucket(ctx context.Context, name string) string {
	return subj.NS("MsgTx_%s_", "default") + name
}
