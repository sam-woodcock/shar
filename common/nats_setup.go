package common

import (
	"github.com/nats-io/nats.go"
	"gitlab.com/shar-workflow/shar/server/messages"
)

func SetUpNats(js nats.JetStreamContext, storageType nats.StorageType) error {
	scfg := &nats.StreamConfig{
		Name:      "WORKFLOW",
		Subjects:  messages.AllMessages,
		Storage:   storageType,
		Retention: nats.InterestPolicy,
	}

	ccfg := &nats.ConsumerConfig{
		Durable:         "Traversal",
		Description:     "Traversal processing queue",
		AckPolicy:       nats.AckExplicitPolicy,
		FilterSubject:   messages.WorkflowTraversalExecute,
		MaxRequestBatch: 1,
		MaxAckPending:   -1,
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
		MaxAckPending:   -1,
	}

	if err := EnsureStream(js, scfg); err != nil {
		return err
	}
	if err := EnsureConsumer(js, "WORKFLOW", ccfg); err != nil {
		return err
	}
	if err := EnsureConsumer(js, "WORKFLOW", tcfg); err != nil {
		return err
	}
	if err := EnsureConsumer(js, "WORKFLOW", acfg); err != nil {
		return err
	}
	return nil
}
