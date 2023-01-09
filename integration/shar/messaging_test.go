package intTest

import (
	"context"
	"fmt"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gitlab.com/shar-workflow/shar/client"
	"gitlab.com/shar-workflow/shar/common/workflow"
	support "gitlab.com/shar-workflow/shar/integration-support"
	"gitlab.com/shar-workflow/shar/model"
	"gitlab.com/shar-workflow/shar/server/messages"
)

//goland:noinspection GoNilness
func TestMessaging(t *testing.T) {
	tst := &support.Integration{}
	tst.Setup(t, nil, nil)
	defer tst.Teardown()

	// Create a starting context
	ctx := context.Background()

	handlers := &testMessagingHandlerDef{wg: sync.WaitGroup{}, tst: tst}

	// Dial shar
	cl := client.New(client.WithEphemeralStorage(), client.WithConcurrency(10))
	err := cl.Dial(tst.NatsURL)
	require.NoError(t, err)

	// Load BPMN workflow
	b, err := os.ReadFile("../../testdata/message-workflow.bpmn")
	require.NoError(t, err)

	_, err = cl.LoadBPMNWorkflowFromBytes(ctx, "TestMessaging", b)
	require.NoError(t, err)

	complete := make(chan *model.WorkflowInstanceComplete, 100)

	// Register a service task
	err = cl.RegisterServiceTask(ctx, "step1", handlers.step1)
	require.NoError(t, err)
	err = cl.RegisterServiceTask(ctx, "step2", handlers.step2)
	require.NoError(t, err)
	err = cl.RegisterMessageSender(ctx, "TestMessaging", "continueMessage", handlers.sendMessage)
	require.NoError(t, err)
	cl.RegisterWorkflowInstanceComplete(complete)

	// Launch the workflow
	wfid, _, err := cl.LaunchWorkflow(ctx, "TestMessaging", model.Vars{"orderId": 57})
	if err != nil {
		t.Fatal(err)
		return
	}
	fmt.Println("Started", wfid)

	// Listen for service tasks
	go func() {
		err := cl.Listen(ctx)
		require.NoError(t, err)
	}()
	select {
	case c := <-complete:
		fmt.Println("completed " + c.WorkflowInstanceId)
	case <-time.After(5 * time.Second):
		require.Fail(t, "no ")
	}
	tst.Mx.Lock()
	assert.Equal(t, "carried1value", tst.FinalVars["carried"])
	assert.Equal(t, "carried2value", tst.FinalVars["carried2"])
	tst.Mx.Unlock()
	tst.AssertCleanKV()
}

type testMessagingHandlerDef struct {
	wg  sync.WaitGroup
	tst *support.Integration
}

func (x *testMessagingHandlerDef) step1(ctx context.Context, client client.JobClient, _ model.Vars) (model.Vars, workflow.WrappedError) {
	if err := client.Log(ctx, messages.LogInfo, -1, "Step 1", nil); err != nil {
		return nil, fmt.Errorf("failed to log: %w", err)
	}
	return model.Vars{}, nil
}

func (x *testMessagingHandlerDef) step2(ctx context.Context, client client.JobClient, vars model.Vars) (model.Vars, workflow.WrappedError) {
	if err := client.Log(ctx, messages.LogInfo, -1, "Step 2", nil); err != nil {
		return nil, fmt.Errorf("failed to log: %w", err)
	}
	x.tst.Mx.Lock()
	x.tst.FinalVars = vars
	x.tst.Mx.Unlock()
	return model.Vars{}, nil
}

func (x *testMessagingHandlerDef) sendMessage(ctx context.Context, client client.MessageClient, vars model.Vars) error {
	if err := client.Log(ctx, messages.LogDebug, -1, "Sending Message...", nil); err != nil {
		return fmt.Errorf("failed to log: %w", err)
	}
	if err := client.SendMessage(ctx, "continueMessage", 57, model.Vars{"carried": vars["carried"]}); err != nil {
		return fmt.Errorf("failed to send continue message: %w", err)
	}
	return nil
}
