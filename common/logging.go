package common

import (
	"context"
	"fmt"
	"github.com/nats-io/nats.go"
	"github.com/segmentio/ksuid"
	"gitlab.com/shar-workflow/shar/common/subj"
	"gitlab.com/shar-workflow/shar/model"
	"gitlab.com/shar-workflow/shar/server/messages"
	"google.golang.org/protobuf/proto"
)

// Log is the generic metod to output to SHAR telemetry.
func Log(ctx context.Context, js nats.JetStream, trackingID string, source model.LogSource, severity messages.WorkflowLogLevel, code int32, message string, attrs map[string]string) error {

	tl := &model.TelemetryLogEntry{
		TrackingID: trackingID,
		Source:     source,
		Message:    message,
		Code:       code,
		Attributes: attrs,
	}
	b, err := proto.Marshal(tl)
	if err != nil {
		return fmt.Errorf("marshal for shar logging: %w", err)
	}
	sub := subj.NS(messages.WorkflowLog, "default") + string(severity)
	if _, err := js.Publish(sub, b, nats.MsgId(ksuid.New().String()), nats.Context(ctx)); err != nil {
		return fmt.Errorf("log publish failed: %w", err)
	}
	return nil
}
