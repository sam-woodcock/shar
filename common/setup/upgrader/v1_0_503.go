package upgrader

import (
	"context"
	"fmt"
	"github.com/nats-io/nats.go"
	"gitlab.com/shar-workflow/shar/common"
	"gitlab.com/shar-workflow/shar/model"
	"gitlab.com/shar-workflow/shar/server/messages"
)

var ver = "1.0.503"

/*
In this upgrade the database changes are as follows:
- The task KV has changed from a string to a model.TaskInfo
*/

//goland:noinspection GoSnakeCaseUsage
func v1_0_503(ctx context.Context, nc common.NatsConn, js nats.JetStreamContext) error {
	// *Upgrade data in service task registry
	kv, err := js.KeyValue(messages.KvClientTaskID)
	if err != nil {
		return fmt.Errorf("upgrade %s getting service task key value store: %w", ver, err)
	}
	keys, err := kv.Keys()
	if err != nil {
		return fmt.Errorf("upgrade %s getting keys: %w", ver, err)
	}
	for _, k := range keys {
		val, err := common.Load(ctx, kv, k)
		if err != nil {
			return fmt.Errorf("upgrade %s getting key %s: %w", ver, k, err)
		}
		if err := common.SaveObj(ctx, kv, k, &model.TaskInfo{
			Id: string(val),
		}); err != nil {
			return fmt.Errorf("upgrading value %+v in %s to %s: %w", val, k, ver, err)
		}
	}
	return nil
}
