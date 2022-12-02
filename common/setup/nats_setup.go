package setup

import (
	"errors"
	"fmt"
	"time"

	"github.com/nats-io/nats.go"
	"gitlab.com/shar-workflow/shar/common/subj"
	"gitlab.com/shar-workflow/shar/server/messages"
)

var consumerConfig []*nats.ConsumerConfig

// ConsumerDurableNames is a list of all consumers used by the engine
var ConsumerDurableNames map[string]struct{}

func init() {
	consumerConfig = []*nats.ConsumerConfig{
		{
			Durable:         "JobAbortConsumer",
			Description:     "Abort job message queue",
			AckPolicy:       nats.AckExplicitPolicy,
			AckWait:         30 * time.Second,
			MaxAckPending:   65535,
			FilterSubject:   subj.NS(messages.WorkFlowJobAbortAll, "*"),
			MaxRequestBatch: 1,
		},
		{
			Durable:         "GeneralAbortConsumer",
			Description:     "Abort workflo instance and activity message queue",
			AckPolicy:       nats.AckExplicitPolicy,
			AckWait:         30 * time.Second,
			MaxAckPending:   65535,
			FilterSubject:   subj.NS(messages.WorkflowGeneralAbortAll, "*"),
			MaxRequestBatch: 1,
		},
		{
			Durable:         "LaunchConsumer",
			Description:     "Sub workflow launch message queue",
			AckPolicy:       nats.AckExplicitPolicy,
			AckWait:         30 * time.Second,
			MaxAckPending:   65535,
			FilterSubject:   subj.NS(messages.WorkflowJobLaunchExecute, "*"),
			MaxRequestBatch: 1,
		},
		{
			Durable:         "Message",
			Description:     "Intra-workflow message queue",
			AckPolicy:       nats.AckExplicitPolicy,
			AckWait:         30 * time.Second,
			MaxAckPending:   65535,
			FilterSubject:   subj.NS(messages.WorkflowMessages, "*"),
			MaxRequestBatch: 1,
		},
		{
			Durable:         "WorkflowConsumer",
			Description:     "Workflow processing queue",
			AckPolicy:       nats.AckExplicitPolicy,
			AckWait:         30 * time.Second,
			MaxAckPending:   65535,
			FilterSubject:   subj.NS(messages.WorkflowInstanceAll, "*"),
			MaxRequestBatch: 1,
		},
		{
			Durable:         "JobCompleteConsumer",
			Description:     "Job complete processing queue",
			AckPolicy:       nats.AckExplicitPolicy,
			AckWait:         30 * time.Second,
			MaxAckPending:   65535,
			FilterSubject:   subj.NS(messages.WorkFlowJobCompleteAll, "*"),
			MaxRequestBatch: 1,
		},
		{
			Durable:         "ActivityConsumer",
			Description:     "Activity complete processing queue",
			AckPolicy:       nats.AckExplicitPolicy,
			AckWait:         30 * time.Second,
			MaxAckPending:   65535,
			FilterSubject:   subj.NS(messages.WorkflowActivityAll, "*"),
			MaxRequestBatch: 1,
		},
		{
			Durable:         "Traversal",
			Description:     "Traversal processing queue",
			AckPolicy:       nats.AckExplicitPolicy,
			AckWait:         30 * time.Second,
			MaxAckPending:   65535,
			FilterSubject:   subj.NS(messages.WorkflowTraversalExecute, "*"),
			MaxRequestBatch: 1,
		},
		{
			Durable:         "Tracking",
			Description:     "Tracking queue for sequential processing",
			AckPolicy:       nats.AckExplicitPolicy,
			AckWait:         30 * time.Second,
			FilterSubject:   "WORKFLOW.>",
			MaxAckPending:   1,
			MaxRequestBatch: 1,
		},
	}

	ConsumerDurableNames = make(map[string]struct{}, len(consumerConfig))
	for _, v := range consumerConfig {
		ConsumerDurableNames[v.Durable] = struct{}{}
	}
}

// EnsureWorkflowStream ensures that the workflow stream exists
func EnsureWorkflowStream(js nats.JetStreamContext, storageType nats.StorageType) error {
	scfg := &nats.StreamConfig{
		Name:      "WORKFLOW",
		Subjects:  messages.AllMessages,
		Storage:   storageType,
		Retention: nats.InterestPolicy,
	}

	if err := ensureStream(js, scfg); err != nil {
		return fmt.Errorf("failed to ensure workflow stream: %w", err)
	}
	for _, ccfg := range consumerConfig {
		if err := ensureConsumer(js, "WORKFLOW", ccfg); err != nil {
			return fmt.Errorf("failed to ensure consumer during ensure workflow stream: %w", err)
		}
	}
	return nil
}

func ensureConsumer(js nats.JetStreamContext, streamName string, consumerConfig *nats.ConsumerConfig) error {
	if _, err := js.ConsumerInfo(streamName, consumerConfig.Durable); errors.Is(err, nats.ErrConsumerNotFound) {
		if _, err := js.AddConsumer(streamName, consumerConfig); err != nil {
			return fmt.Errorf("cannot ensure consumer '%s' with subject '%s' : %w", consumerConfig.Name, consumerConfig.FilterSubject, err)
		}
	} else if err != nil {
		return fmt.Errorf("could not ensure consumer: %w", err)
	}
	return nil
}

func ensureStream(js nats.JetStreamContext, streamConfig *nats.StreamConfig) error {
	if _, err := js.StreamInfo(streamConfig.Name); errors.Is(err, nats.ErrStreamNotFound) {
		if _, err := js.AddStream(streamConfig); err != nil {
			return fmt.Errorf("could not add stream: %w", err)
		}
	} else if err != nil {
		return fmt.Errorf("could not ensure stream: %w", err)
	}
	return nil
}
