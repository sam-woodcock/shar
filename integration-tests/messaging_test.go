package intTests

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
	"sync"
	"testing"
	"time"
)

//goland:noinspection GoNilness
func TestMessaging(t *testing.T) {
	tst := &integration{}
	tst.setup(t)
	defer tst.teardown()

	// Create a starting context
	ctx := context.Background()

	// Create logger
	log, _ := zap.NewDevelopment()
	handlers := &testMessagingHandlerDef{log: log, wg: sync.WaitGroup{}, tst: tst}

	// Dial shar
	cl := client.New(log, client.WithEphemeralStorage())
	err := cl.Dial(natsURL)
	require.NoError(t, err)

	// Load BPMN workflow
	b, err := os.ReadFile("../testdata/message-workflow.bpmn")
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
	if wfid, err := cl.LaunchWorkflow(ctx, "TestMessaging", model.Vars{"orderId": 57}); err != nil {
		t.Fatal(err)
		return
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
	tst.mx.Lock()
	assert.Equal(t, "carried1value", tst.finalVars["carried"])
	assert.Equal(t, "carried2value", tst.finalVars["carried2"])
	tst.mx.Unlock()
	tst.AssertCleanKV()
}

type testMessagingHandlerDef struct {
	log *zap.Logger
	wg  sync.WaitGroup
	tst *integration
}

func (x *testMessagingHandlerDef) step1(ctx context.Context, client *client.JobClient, vars model.Vars) (model.Vars, error) {
	if err := client.Log(ctx, messages.LogInfo, -1, "Step 1", nil); err != nil {
		return nil, err
	}
	return model.Vars{}, nil
}

func (x *testMessagingHandlerDef) step2(ctx context.Context, client *client.JobClient, vars model.Vars) (model.Vars, error) {
	if err := client.Log(ctx, messages.LogInfo, -1, "Step 2", nil); err != nil {
		return nil, err
	}
	x.tst.mx.Lock()
	x.tst.finalVars = vars
	x.tst.mx.Unlock()
	return model.Vars{}, nil
}

func (x *testMessagingHandlerDef) sendMessage(ctx context.Context, client *client.MessageClient, vars model.Vars) error {
	if err := client.Log(ctx, messages.LogDebug, -1, "Sending Message...", nil); err != nil {
		return err
	}
	return client.SendMessage(ctx, "continueMessage", 57, model.Vars{"carried": vars["carried"]})
}
