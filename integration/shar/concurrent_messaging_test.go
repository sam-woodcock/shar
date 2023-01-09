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
)

//goland:noinspection GoNilness
func TestConcurrentMessaging(t *testing.T) {
	tst := &support.Integration{}
	tst.Setup(t, nil, nil)
	defer tst.Teardown()
	tst.Cooldown = 5 * time.Second
	//tracer.Trace("127.0.0.1:4459")
	//defer tracer.Close()

	handlers := &testConcurrentMessagingHandlerDef{}
	handlers.tst = tst
	// Create a starting context
	ctx := context.Background()

	// Dial shar
	cl := client.New(client.WithEphemeralStorage(), client.WithConcurrency(10))
	err := cl.Dial(tst.NatsURL)
	require.NoError(t, err)

	// Load BPMN workflow
	b, err := os.ReadFile("../../testdata/message-workflow.bpmn")
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
			if wfiID, _, err := cl.LaunchWorkflow(ctx, "TestConcurrentMessaging", model.Vars{"orderId": 57}); err != nil {
				panic(err)
			} else {
				instances[wfiID] = struct{}{}
			}
		}()
	}
	for inst := 0; inst < n; inst++ {
		select {
		case c := <-complete:
			fmt.Println(c.WorkflowInstanceId)
		case <-time.After(5 * time.Second):
			require.Fail(t, "timed out")
		}
	}
	assert.Equal(t, handlers.received, n)
	fmt.Println("Stopwatch:", -time.Until(tm))
	tst.AssertCleanKV()
}

type testConcurrentMessagingHandlerDef struct {
	mx       sync.Mutex
	tst      *support.Integration
	received int
}

func (x *testConcurrentMessagingHandlerDef) step1(_ context.Context, _ client.JobClient, _ model.Vars) (model.Vars, workflow.WrappedError) {
	return model.Vars{}, nil
}

func (x *testConcurrentMessagingHandlerDef) step2(_ context.Context, _ client.JobClient, vars model.Vars) (model.Vars, workflow.WrappedError) {
	assert.Equal(x.tst.Test, "carried1value", vars["carried"])
	assert.Equal(x.tst.Test, "carried2value", vars["carried2"])
	x.mx.Lock()
	defer x.mx.Unlock()
	x.received++
	return model.Vars{}, nil
}

func (x *testConcurrentMessagingHandlerDef) sendMessage(ctx context.Context, cmd client.MessageClient, vars model.Vars) error {
	if err := cmd.SendMessage(ctx, "continueMessage", 57, model.Vars{"carried": vars["carried"]}); err != nil {
		return fmt.Errorf("failed to send continue message: %w", err)
	}
	return nil
}
