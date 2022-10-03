package intTests

import (
	"context"
	"fmt"
	"github.com/stretchr/testify/require"
	"gitlab.com/shar-workflow/shar/client"
	"gitlab.com/shar-workflow/shar/model"
	"go.uber.org/zap"
	"os"
	"testing"
	"time"
)

//goland:noinspection GoNilness
func TestConcurrentMessaging(t *testing.T) {
	tst := &integration{}
	tst.setup(t)
	defer tst.teardown()

	handlers := &testConcurrentMessagingHandlerDef{}

	// Create a starting context
	ctx := context.Background()

	// Create logger
	log, _ := zap.NewDevelopment()

	// Dial shar
	cl := client.New(log, client.WithEphemeralStorage())
	err := cl.Dial(natsURL)
	require.NoError(t, err)

	// Load BPMN workflow
	b, err := os.ReadFile("../testdata/message-workflow.bpmn")
	require.NoError(t, err)

	_, err = cl.LoadBPMNWorkflowFromBytes(ctx, "TestConcurrentMessaging", b)
	require.NoError(t, err)

	complete := make(chan *model.WorkflowInstanceComplete, 101)

	// Register a service task
	err = cl.RegisterServiceTask(ctx, "step1", handlers.step1)
	require.NoError(t, err)
	err = cl.RegisterServiceTask(ctx, "step2", handlers.step2)
	require.NoError(t, err)
	err = cl.RegisterMessageSender(ctx, "TestConcurrentMessaging", "continueMessage", handlers.sendMessage)
	require.NoError(t, err)
	cl.RegisterWorkflowInstanceComplete(complete)

	// Listen for service tasks
	go func() {
		err := cl.Listen(ctx)
		require.NoError(t, err)
	}()

	instances := make(map[string]struct{})

	n := 50
	tm := time.Now()
	for inst := 0; inst < n; inst++ {
		go func() {
			// Launch the workflow
			if wfiID, err := cl.LaunchWorkflow(ctx, "TestConcurrentMessaging", model.Vars{"orderId": 57}); err != nil {
				panic(err)
			} else {
				instances[wfiID] = struct{}{}
			}
		}()
	}
	for inst := 0; inst < n; inst++ {
		select {
		case c := <-complete:
			fmt.Println("completed " + c.WorkflowInstanceId)
		case <-time.After(120 * time.Second):
		}
	}
	fmt.Println("Stopwatch:", -time.Until(tm))
	/*
		// Check consistency
		js, err := GetJetstream()
		require.NoError(t, err)

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
	*/
}

type testConcurrentMessagingHandlerDef struct {
}

func (x *testConcurrentMessagingHandlerDef) step1(_ context.Context, _ model.Vars) (model.Vars, error) {
	fmt.Println("Step 1")
	time.Sleep(1 * time.Second)
	return model.Vars{}, nil
}

func (x *testConcurrentMessagingHandlerDef) step2(_ context.Context, _ model.Vars) (model.Vars, error) {
	fmt.Println("Step 2")
	time.Sleep(1 * time.Second)
	return model.Vars{}, nil
}

func (x *testConcurrentMessagingHandlerDef) sendMessage(ctx context.Context, cmd *client.Command, _ model.Vars) error {

	fmt.Println("Sending Message...")
	return cmd.SendMessage(ctx, "continueMessage", 57, model.Vars{})
}
