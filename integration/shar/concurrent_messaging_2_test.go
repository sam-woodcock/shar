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
	"strconv"
	"sync"
	"testing"
	"time"
)

//goland:noinspection GoNilness
func TestConcurrentMessaging2(t *testing.T) {
	tst := &support.Integration{
		Cooldown: time.Second * 10,
	}
	//tst.WithTrace = true
	tst.Setup(t, nil, nil)
	defer tst.Teardown()
	tst.Cooldown = 5 * time.Second

	handlers := &testConcurrentMessaging2HandlerDef{finished: make(chan struct{})}
	handlers.tst = tst
	// Create a starting context
	ctx := context.Background()

	// Dial shar
	cl := client.New(client.WithEphemeralStorage(), client.WithConcurrency(10))
	err := cl.Dial(tst.NatsURL)
	require.NoError(t, err)

	// Load BPMN workflow
	b, err := os.ReadFile("../../testdata/message-workflow-2.bpmn")
	require.NoError(t, err)

	_, err = cl.LoadBPMNWorkflowFromBytes(ctx, "TestConcurrentMessaging", b)
	require.NoError(t, err)

	// Register a service task
	err = cl.RegisterServiceTask(ctx, "step1", handlers.step1)
	require.NoError(t, err)
	err = cl.RegisterServiceTask(ctx, "step2", handlers.step2)
	require.NoError(t, err)
	err = cl.RegisterMessageSender(ctx, "TestConcurrentMessaging", "continueMessage", handlers.sendMessage)
	require.NoError(t, err)
	err = cl.RegisterProcessComplete("Process_03llwnm", handlers.processEnd)
	require.NoError(t, err)
	// Listen for service tasks
	go func() {
		err := cl.Listen(ctx)
		require.NoError(t, err)
	}()

	handlers.instComplete = make(map[string]struct{})
	n := 100
	launch := 0
	tm := time.Now()
	for inst := 0; inst < n; inst++ {
		go func(inst int) {
			// Launch the workflow
			if _, _, err := cl.LaunchWorkflow(ctx, "TestConcurrentMessaging", model.Vars{"orderId": inst}); err != nil {
				panic(err)
			} else {
				handlers.mx.Lock()
				launch++
				handlers.instComplete[strconv.Itoa(inst)] = struct{}{}
				handlers.mx.Unlock()
			}
		}(inst)
	}
	for inst := 0; inst < n; inst++ {
		support.WaitForChan(t, handlers.finished, 20*time.Second)
	}
	fmt.Println("Stopwatch:", -time.Until(tm))
	tst.AssertCleanKV()
	assert.Equal(t, launch, handlers.received)
	assert.Equal(t, 0, len(handlers.instComplete))
}

type testConcurrentMessaging2HandlerDef struct {
	mx           sync.Mutex
	tst          *support.Integration
	received     int
	finished     chan struct{}
	instComplete map[string]struct{}
}

func (x *testConcurrentMessaging2HandlerDef) step1(_ context.Context, _ client.JobClient, _ model.Vars) (model.Vars, error) {
	return model.Vars{}, nil
}

func (x *testConcurrentMessaging2HandlerDef) step2(_ context.Context, _ client.JobClient, vars model.Vars) (model.Vars, error) {
	assert.Equal(x.tst.Test, "carried1value", vars["carried"])
	assert.Equal(x.tst.Test, "carried2value", vars["carried2"])
	return model.Vars{}, nil
}

func (x *testConcurrentMessaging2HandlerDef) sendMessage(ctx context.Context, cmd client.MessageClient, vars model.Vars) error {
	if err := cmd.SendMessage(ctx, "continueMessage", vars["orderId"], model.Vars{"carried": vars["carried"]}); err != nil {
		return fmt.Errorf("send continue message: %w", err)
	}
	return nil
}

func (x *testConcurrentMessaging2HandlerDef) processEnd(ctx context.Context, vars model.Vars, wfError *model.Error, state model.CancellationState) {
	x.finished <- struct{}{}
	x.mx.Lock()
	if _, ok := x.instComplete[strconv.Itoa(vars["orderId"].(int))]; !ok {
		panic("too many calls")
	}
	delete(x.instComplete, strconv.Itoa(vars["orderId"].(int)))
	x.received++
	x.mx.Unlock()
}
