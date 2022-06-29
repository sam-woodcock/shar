package integration_tests

import (
	"context"
	"fmt"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gitlab.com/shar-workflow/shar/client"
	"gitlab.com/shar-workflow/shar/model"
	"gitlab.com/shar-workflow/shar/server/messages"
	"go.uber.org/zap"
	"os"
	"testing"
)

func TestMessaging(t *testing.T) {
	// Create a starting context
	ctx := context.Background()

	// Create logger
	log, _ := zap.NewDevelopment()

	defer func() {
		if err := log.Sync(); err != nil {
		}
	}()

	// Dial shar
	cl := client.New(log, client.EphemeralStorage{})
	err := cl.Dial("nats://127.0.0.1:4459")
	require.NoError(t, err)

	// Load BPMN workflow
	b, err := os.ReadFile("../testdata/message-workflow.bpmn")
	require.NoError(t, err)

	_, err = cl.LoadBPMNWorkflowFromBytes(ctx, "MessageDemo", b)
	require.NoError(t, err)

	complete := make(chan *model.WorkflowInstanceComplete, 100)

	// Register a service task
	cl.RegisterServiceTask("step1", step1)
	cl.RegisterServiceTask("step2", step2)
	cl.RegisterMessageSender("continueMessage", sendMessage)
	cl.RegisterWorkflowInstanceComplete(complete)

	// Launch the workflow
	if _, err := cl.LaunchWorkflow(ctx, "MessageDemo", model.Vars{"orderId": 57}); err != nil {
		panic(err)
	}

	// Listen for service tasks
	go func() {
		err := cl.Listen(ctx)
		require.NoError(t, err)
	}()
	select {
	case c := <-complete:
		fmt.Println("completed " + c.WorkflowInstanceId)
	}

	// Check consistency
	js, err := GetJetstream()

	getKeys := func(kv string) ([]string, error) {
		messageSubs, err := js.KeyValue(kv)
		if err != nil {
			return nil, err
		}
		k, err := messageSubs.Keys()
		if err != nil {
			return nil, err
		}
		return k, nil
	}

	// Check cleanup
	ids, err := getKeys(messages.KvMessageID)
	assert.NoError(t, err)
	assert.Equal(t, 1, len(ids))
	_, err = getKeys(messages.KvMessageSubs)
	assert.Equal(t, err.Error(), "nats: no keys found")
	_, err = getKeys(messages.KvMessageSub)
	assert.Equal(t, err.Error(), "nats: no keys found")
	_, err = getKeys(messages.KvInstance)
	assert.Equal(t, err.Error(), "nats: no keys found")

}

func step1(ctx context.Context, vars model.Vars) (model.Vars, error) {
	fmt.Println("Step 1")
	return model.Vars{}, nil
}

func step2(ctx context.Context, vars model.Vars) (model.Vars, error) {
	fmt.Println("Step 2")
	return model.Vars{}, nil
}

func sendMessage(ctx context.Context, cmd *client.Command, vars model.Vars) error {
	fmt.Println("Sending Message...")
	return cmd.SendMessage(ctx, "continueMessage", 57, model.Vars{})
}
