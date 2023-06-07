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
func TestStartMessaging(t *testing.T) {
	tst := &support.Integration{}
	//tst.WithTrace = true
	tst.Setup(t, nil, nil)

	defer tst.Teardown()

	// Create a starting context
	ctx := context.Background()

	handlers := &testStartMessagingHandlerDef{t: t, wg: sync.WaitGroup{}, tst: tst, finished: make(chan struct{})}

	// Dial shar
	cl := client.New(client.WithEphemeralStorage(), client.WithConcurrency(10))
	err := cl.Dial(ctx, tst.NatsURL)
	require.NoError(t, err)

	// Load BPMN workflow
	b, err := os.ReadFile("../../testdata/message-start-test.bpmn")
	require.NoError(t, err)

	_, err = cl.LoadBPMNWorkflowFromBytes(ctx, "TestMessaging", b)
	require.NoError(t, err)

	// Register a service task
	err = cl.RegisterServiceTask(ctx, "SimpleService", handlers.simpleService)
	require.NoError(t, err)
	err = cl.RegisterProcessComplete("Process_0w6dssp", handlers.processEnd)
	require.NoError(t, err)

	// Listen for service tasks
	go func() {
		err := cl.Listen(ctx)
		require.NoError(t, err)
	}()

	err = cl.SendMessage(ctx, "startDemoMsg", nil, model.Vars{})
	require.NoError(t, err)

	support.WaitForChan(t, handlers.finished, 20*time.Second)

	tst.AssertCleanKV()
}

type testStartMessagingHandlerDef struct {
	wg       sync.WaitGroup
	tst      *support.Integration
	finished chan struct{}
	t        *testing.T
}

func (x *testStartMessagingHandlerDef) simpleService(ctx context.Context, client client.JobClient, _ model.Vars) (model.Vars, error) {
	if err := client.Log(ctx, messages.LogInfo, -1, "Step 1", nil); err != nil {
		return nil, fmt.Errorf("log: %w", err)
	}
	return model.Vars{}, nil
}

func (x *testStartMessagingHandlerDef) processEnd(ctx context.Context, vars model.Vars, wfError *model.Error, state model.CancellationState) {

	assert.Equal(x.t, "carried1value", vars["carried"])
	assert.Equal(x.t, "carried2value", vars["carried2"])
	close(x.finished)
}
