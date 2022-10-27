package setup

import (
	"fmt"
	"time"

	"github.com/nats-io/nats.go"
	"gitlab.com/shar-workflow/shar/common/subj"
	"gitlab.com/shar-workflow/shar/server/messages"
)

var consumerConfig []*nats.ConsumerConfig
var ConsumerDurableNames map[string]struct{}

func init() {
	consumerConfig = []*nats.ConsumerConfig{
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
		{
			Durable:         "API",
			Description:     "Api queue",
			AckPolicy:       nats.AckExplicitPolicy,
			AckWait:         30 * time.Second,
			FilterSubject:   messages.ApiAll,
			MaxRequestBatch: 1,
			MaxAckPending:   -1,
		},
		{
			Durable:       "Tracing",
			Description:   "Sequential Trace Consumer1",
			DeliverPolicy: nats.DeliverAllPolicy,
			FilterSubject: subj.NS(messages.WorkflowStateAll, "*"),
			AckPolicy:     nats.AckExplicitPolicy,
			MaxAckPending: 1,
		},
	}

	ConsumerDurableNames = make(map[string]struct{}, len(consumerConfig))
	for _, v := range consumerConfig {
		ConsumerDurableNames[v.Durable] = struct{}{}
	}
}

func Nats(js nats.JetStreamContext, storageType nats.StorageType) error {
	scfg := &nats.StreamConfig{
		Name:      "WORKFLOW",
		Subjects:  messages.AllMessages,
		Storage:   storageType,
		Retention: nats.InterestPolicy,
	}

	if err := EnsureStream(js, scfg); err != nil {
		return err
	}
	for _, ccfg := range consumerConfig {
		if err := EnsureConsumer(js, "WORKFLOW", ccfg); err != nil {
			return err
		}
	}
	return nil
}

func EnsureConsumer(js nats.JetStreamContext, streamName string, consumerConfig *nats.ConsumerConfig) error {
	if _, err := js.ConsumerInfo(streamName, consumerConfig.Durable); err == nats.ErrConsumerNotFound {
		if _, err := js.AddConsumer(streamName, consumerConfig); err != nil {
			return fmt.Errorf("cannot ensure consumer '%s' with subject '%s' : %w", consumerConfig.Name, consumerConfig.FilterSubject, err)
		}
	} else if err != nil {
		return err
	}
	return nil
}

func EnsureStream(js nats.JetStreamContext, streamConfig *nats.StreamConfig) error {
	if _, err := js.StreamInfo(streamConfig.Name); err == nats.ErrStreamNotFound {
		if _, err := js.AddStream(streamConfig); err != nil {
			return err
		}
	} else if err != nil {
		return err
	}
	return nil
}
