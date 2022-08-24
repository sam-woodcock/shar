package main

import (
	"context"
	"fmt"
	"github.com/stretchr/testify/require"
	"gitlab.com/shar-workflow/shar/client"
	"gitlab.com/shar-workflow/shar/model"
	"go.uber.org/zap"
	"os"
	"sync"
	"testing"
	"time"
)

//goland:noinspection GoNilness
func TestMessaging(t *testing.T) {
	//	if os.Getenv("INT_TEST") != "true" {
	//		t.Skip("Skipping integration test " + t.Name())
	//	}

	tst := &integration{}
	tst.setup(t)
	defer tst.teardown()

	// Create a starting context
	ctx := context.Background()

	// Create logger
	log, _ := zap.NewDevelopment()
	handlers := &testMessagingHandlerDef{log: log, wg: sync.WaitGroup{}}

	// Dial shar
	cl := client.New(log, client.EphemeralStorage{})
	err := cl.Dial(natsURL)
	require.NoError(t, err)

	// Load BPMN workflow
	b, err := os.ReadFile("../testdata/message-workflow.bpmn")
	require.NoError(t, err)

	_, err = cl.LoadBPMNWorkflowFromBytes(ctx, "TestMessaging", b)
	require.NoError(t, err)

	complete := make(chan *model.WorkflowInstanceComplete, 100)

	// Register a service task
	cl.RegisterServiceTask("step1", handlers.step1)
	cl.RegisterServiceTask("step2", handlers.step2)
	cl.RegisterMessageSender("continueMessage", handlers.sendMessage)
	cl.RegisterWorkflowInstanceComplete(complete)

	// Launch the workflow
	if wfid, err := cl.LaunchWorkflow(ctx, "TestMessaging", model.Vars{"orderId": 57}); err != nil {
		t.Fatal(err)
	} else {
		fmt.Println("Started", wfid)
	}

	// Listen for service tasks
	go func() {
		err := cl.Listen(ctx)
		require.NoError(t, err)
	}()
	select {
	case c := <-complete:
		fmt.Println("completed " + c.WorkflowInstanceId)
	case <-time.After(20 * time.Second):
	}
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

type testMessagingHandlerDef struct {
	log *zap.Logger
	wg  sync.WaitGroup
}

func (x *testMessagingHandlerDef) step1(_ context.Context, _ model.Vars) (model.Vars, error) {
	x.log.Info("Step 1")
	return model.Vars{}, nil
}

func (x *testMessagingHandlerDef) step2(_ context.Context, _ model.Vars) (model.Vars, error) {
	x.log.Info("Step 2")
	return model.Vars{}, nil
}

func (x *testMessagingHandlerDef) sendMessage(ctx context.Context, cmd *client.Command, _ model.Vars) error {
	x.log.Info("Sending Message...")
	return cmd.SendMessage(ctx, "continueMessage", 57, model.Vars{})
}
