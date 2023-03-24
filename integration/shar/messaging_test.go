package intTest

import (
	"context"
	"fmt"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gitlab.com/shar-workflow/shar/client"
	support "gitlab.com/shar-workflow/shar/integration-support"
	"gitlab.com/shar-workflow/shar/model"
	"gitlab.com/shar-workflow/shar/server/messages"
	"os"
	"sync"
	"testing"
	"time"
)

//goland:noinspection GoNilness
func TestMessaging(t *testing.T) {
	tst := &support.Integration{}
	tst.WithTrace = true
	tst.Setup(t, nil, nil)

	defer tst.Teardown()

	// Create a starting context
	ctx := context.Background()

	handlers := &testMessagingHandlerDef{t: t, wg: sync.WaitGroup{}, tst: tst, finished: make(chan struct{})}

	// Dial shar
	cl := client.New(client.WithEphemeralStorage(), client.WithConcurrency(10))
	err := cl.Dial(tst.NatsURL)
	require.NoError(t, err)

	// Load BPMN workflow
	b, err := os.ReadFile("../../testdata/message-workflow.bpmn")
	require.NoError(t, err)

	_, err = cl.LoadBPMNWorkflowFromBytes(ctx, "TestMessaging", b)
	require.NoError(t, err)

	// Register a service task
	err = cl.RegisterServiceTask(ctx, "step1", handlers.step1)
	require.NoError(t, err)
	err = cl.RegisterServiceTask(ctx, "step2", handlers.step2)
	require.NoError(t, err)
	err = cl.RegisterMessageSender(ctx, "TestMessaging", "continueMessage", handlers.sendMessage)
	require.NoError(t, err)
	err = cl.RegisterProcessComplete("Process_03llwnm", handlers.processEnd)
	require.NoError(t, err)

	// Launch the workflow
	_, _, err = cl.LaunchWorkflow(ctx, "TestMessaging", model.Vars{"orderId": 57})
	if err != nil {
		t.Fatal(err)
		return
	}
	// Listen for service tasks
	go func() {
		err := cl.Listen(ctx)
		require.NoError(t, err)
	}()
	support.WaitForChan(t, handlers.finished, 20*time.Second)

	tst.AssertCleanKV()
}

type testMessagingHandlerDef struct {
	wg       sync.WaitGroup
	tst      *support.Integration
	finished chan struct{}
	t        *testing.T
}

func (x *testMessagingHandlerDef) step1(ctx context.Context, client client.JobClient, _ model.Vars) (model.Vars, error) {
	if err := client.Log(ctx, messages.LogInfo, -1, "Step 1", nil); err != nil {
		return nil, fmt.Errorf("log: %w", err)
	}
	return model.Vars{}, nil
}

func (x *testMessagingHandlerDef) step2(ctx context.Context, client client.JobClient, vars model.Vars) (model.Vars, error) {
	if err := client.Log(ctx, messages.LogInfo, -1, "Step 2", nil); err != nil {
		return nil, fmt.Errorf("log: %w", err)
	}
	x.tst.Mx.Lock()
	x.tst.FinalVars = vars
	x.tst.Mx.Unlock()
	return model.Vars{}, nil
}

func (x *testMessagingHandlerDef) sendMessage(ctx context.Context, client client.MessageClient, vars model.Vars) error {
	if err := client.Log(ctx, messages.LogDebug, -1, "Sending Message...", nil); err != nil {
		return fmt.Errorf("log: %w", err)
	}
	if err := client.SendMessage(ctx, "continueMessage", 57, model.Vars{"carried": vars["carried"]}); err != nil {
		return fmt.Errorf("send continue message: %w", err)
	}
	return nil
}

func (x *testMessagingHandlerDef) processEnd(ctx context.Context, vars model.Vars, wfError *model.Error, state model.CancellationState) {

	assert.Equal(x.t, "carried1value", vars["carried"])
	assert.Equal(x.t, "carried2value", vars["carried2"])
	close(x.finished)
}
