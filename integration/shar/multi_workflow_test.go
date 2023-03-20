package intTest

import (
	"context"
	"fmt"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gitlab.com/shar-workflow/shar/client"
	support "gitlab.com/shar-workflow/shar/integration-support"
	"gitlab.com/shar-workflow/shar/model"
	"os"
	"sync"
	"testing"
	"time"
)

//goland:noinspection GoNilness
func TestMultiWorkflow(t *testing.T) {
	tst := &support.Integration{Cooldown: 5 * time.Second}
	tst.Setup(t, nil, nil)
	defer tst.Teardown()
	handlers := &testMultiworkflowMessagingHandlerDef{t: t, finished: make(chan struct{})}

	// Create a starting context
	ctx := context.Background()

	// Dial shar
	cl := client.New(client.WithEphemeralStorage(), client.WithConcurrency(10))
	err := cl.Dial(tst.NatsURL)
	require.NoError(t, err)

	// Load BPMN workflow
	b, err := os.ReadFile("../../testdata/message-workflow.bpmn")
	require.NoError(t, err)

	// Load BPMN workflow 2
	b2, err := os.ReadFile("../../testdata/simple-workflow.bpmn")
	require.NoError(t, err)

	_, err = cl.LoadBPMNWorkflowFromBytes(ctx, "TestMultiWorkflow1", b)
	require.NoError(t, err)

	_, err = cl.LoadBPMNWorkflowFromBytes(ctx, "TestMultiWorkflow2", b2)
	require.NoError(t, err)

	// Register a service task
	err = cl.RegisterServiceTask(ctx, "step1", handlers.step1)
	require.NoError(t, err)
	err = cl.RegisterServiceTask(ctx, "step2", handlers.step2)
	require.NoError(t, err)
	err = cl.RegisterServiceTask(ctx, "SimpleProcess", handlers.simpleProcess)
	require.NoError(t, err)

	err = cl.RegisterMessageSender(ctx, "TestMultiWorkflow1", "continueMessage", handlers.sendMessage)
	require.NoError(t, err)

	err = cl.RegisterProcessComplete("Process_03llwnm", handlers.processEnd)
	require.NoError(t, err)
	// Listen for service tasks
	go func() {
		err := cl.Listen(ctx)
		require.NoError(t, err)
	}()
	n := 100
	mx := sync.Mutex{}
	instances := make(map[string]struct{})
	wg := sync.WaitGroup{}
	for inst := 0; inst < n; inst++ {
		wg.Add(1)
		go func() {
			// Launch the workflow
			if wfiID, _, err := cl.LaunchWorkflow(ctx, "TestMultiWorkflow1", model.Vars{"orderId": 57}); err != nil {
				panic(err)
			} else {
				mx.Lock()
				instances[wfiID] = struct{}{}
				mx.Unlock()
			}
			if wfiID2, _, err := cl.LaunchWorkflow(ctx, "TestMultiWorkflow2", model.Vars{}); err != nil {
				panic(err)
			} else {
				mx.Lock()
				instances[wfiID2] = struct{}{}
				mx.Unlock()
			}
		}()
	}
	go func() {
		for i := 0; i < n*2; i++ {
			<-handlers.finished
			wg.Done()
		}
	}()
	wg.Wait()
	tst.AssertCleanKV()
}

type testMultiworkflowMessagingHandlerDef struct {
	t        *testing.T
	finished chan struct{}
}

func (x *testMultiworkflowMessagingHandlerDef) step1(_ context.Context, _ client.JobClient, _ model.Vars) (model.Vars, error) {
	return model.Vars{}, nil
}

func (x *testMultiworkflowMessagingHandlerDef) step2(_ context.Context, _ client.JobClient, vars model.Vars) (model.Vars, error) {
	assert.Equal(x.t, "carried1value", vars["carried"].(string))
	assert.Equal(x.t, "carried2value", vars["carried2"].(string))
	return model.Vars{}, nil
}

func (x *testMultiworkflowMessagingHandlerDef) sendMessage(ctx context.Context, cmd client.MessageClient, vars model.Vars) error {
	if err := cmd.SendMessage(ctx, "continueMessage", 57, model.Vars{"carried": vars["carried"]}); err != nil {
		return fmt.Errorf("send continue message: %w", err)
	}
	return nil
}

// A "Hello World" service task
func (x *testMultiworkflowMessagingHandlerDef) simpleProcess(_ context.Context, _ client.JobClient, vars model.Vars) (model.Vars, error) {
	assert.Equal(x.t, 32768, vars["carried"].(int))
	return model.Vars{}, nil
}

func (x *testMultiworkflowMessagingHandlerDef) processEnd(ctx context.Context, vars model.Vars, wfError *model.Error, state model.CancellationState) {
	x.finished <- struct{}{}
}
