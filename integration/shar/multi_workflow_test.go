package intTest

import (
	"context"
	"fmt"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"gitlab.com/shar-workflow/shar/client"
	"gitlab.com/shar-workflow/shar/common/workflow"
	support "gitlab.com/shar-workflow/shar/integration-support"
	"gitlab.com/shar-workflow/shar/model"
)

//goland:noinspection GoNilness
func TestMultiWorkflow(t *testing.T) {
	tst := &support.Integration{}
	tst.Setup(t, nil, nil)
	defer tst.Teardown()
	tst.Cooldown = 5 * time.Second
	handlers := &testMultiworkflowMessagingHandlerDef{}

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

	complete := make(chan *model.WorkflowInstanceComplete, 400)

	// Register a service task
	err = cl.RegisterServiceTask(ctx, "step1", handlers.step1)
	require.NoError(t, err)
	err = cl.RegisterServiceTask(ctx, "step2", handlers.step2)
	require.NoError(t, err)
	err = cl.RegisterServiceTask(ctx, "SimpleProcess", handlers.simpleProcess)
	require.NoError(t, err)

	err = cl.RegisterMessageSender(ctx, "TestMultiWorkflow1", "continueMessage", handlers.sendMessage)
	require.NoError(t, err)

	cl.RegisterWorkflowInstanceComplete(complete)

	// Listen for service tasks
	go func() {
		err := cl.Listen(ctx)
		require.NoError(t, err)
	}()
	n := 25
	mx := sync.Mutex{}
	instances := make(map[string]struct{})
	wg := sync.WaitGroup{}
	for inst := 0; inst < n; inst++ {
		wg.Add(2)
		go func() {
			// Launch the workflow
			if wfiID, _, err := cl.LaunchWorkflow(ctx, "TestMultiWorkflow1", model.Vars{"orderId": 57}); err != nil {
				panic(err)
			} else {
				mx.Lock()
				instances[wfiID] = struct{}{}
				mx.Unlock()
				fmt.Println("started", wfiID)
			}
			if wfiID2, _, err := cl.LaunchWorkflow(ctx, "TestMultiWorkflow2", model.Vars{}); err != nil {
				panic(err)
			} else {
				mx.Lock()
				instances[wfiID2] = struct{}{}
				mx.Unlock()
				fmt.Println("started", wfiID2)
			}
		}()
	}
	go func() {
		for i := 0; i < n*2; i++ {
			c := <-complete
			wg.Done()
			mx.Lock()
			delete(instances, c.WorkflowInstanceId)
			mx.Unlock()
		}
	}()
	wg.Wait()
	tst.AssertCleanKV()
}

type testMultiworkflowMessagingHandlerDef struct {
}

func (x *testMultiworkflowMessagingHandlerDef) step1(_ context.Context, _ client.JobClient, _ model.Vars) (model.Vars, workflow.WrappedError) {
	fmt.Println("Step 1")

	return model.Vars{}, nil
}

func (x *testMultiworkflowMessagingHandlerDef) step2(_ context.Context, _ client.JobClient, vars model.Vars) (model.Vars, workflow.WrappedError) {
	fmt.Println("Step 2")
	fmt.Println("carried", vars["carried"])
	fmt.Println("carried2", vars["carried2"])
	//time.Sleep(1 * time.Second)
	return model.Vars{}, nil
}

func (x *testMultiworkflowMessagingHandlerDef) sendMessage(ctx context.Context, cmd client.MessageClient, vars model.Vars) error {
	fmt.Println("Sending Message...")
	if err := cmd.SendMessage(ctx, "continueMessage", 57, model.Vars{"carried": vars["carried"]}); err != nil {
		return fmt.Errorf("failed to send continue message: %w", err)
	}
	return nil
}

// A "Hello World" service task
func (x *testMultiworkflowMessagingHandlerDef) simpleProcess(_ context.Context, _ client.JobClient, vars model.Vars) (model.Vars, workflow.WrappedError) {
	fmt.Println("Hello World")
	fmt.Println("carried", vars["carried"])
	fmt.Println("carried2", vars["carried2"])
	return model.Vars{}, nil
}
