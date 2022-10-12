package intTests

import (
	"context"
	"fmt"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gitlab.com/shar-workflow/shar/client"
	"gitlab.com/shar-workflow/shar/model"
	"go.uber.org/zap"
	"os"
	"sync"
	"testing"
	"time"
)

func TestBoundaryTimer(t *testing.T) {
	tst := &integration{}
	tst.setup(t)
	defer tst.teardown()

	complete := make(chan *model.WorkflowInstanceComplete, 100)
	d := &testBoundaryTimerDef{}

	executeBoundaryTimerTest(t, complete, d)
	select {
	case c := <-complete:
		fmt.Println("completed " + c.WorkflowInstanceId)
	case <-time.After(20 * time.Second):
		assert.Fail(t, "Timed out")
	}
	fmt.Println("CanTimeOut Called:", d.CanTimeOutCalled)
	fmt.Println("TimedOut Called:", d.TimedOutCalled)
	fmt.Println("CheckResult Called:", d.CheckResultCalled)
}

func TestBoundaryTimerTimeout(t *testing.T) {
	tst := &integration{}
	tst.setup(t)
	defer tst.teardown()

	//sub := tracer.Trace("nats://127.0.0.1:4459")
	//defer sub.Drain()

	complete := make(chan *model.WorkflowInstanceComplete, 100)
	d := &testBoundaryTimerDef{
		CanTimeOutPause:  time.Second * 5,
		CheckResultPause: time.Second * 4,
	}

	executeBoundaryTimerTest(t, complete, d)
	select {
	case c := <-complete:
		fmt.Println("completed " + c.WorkflowInstanceId)
	case <-time.After(20 * time.Second):
		assert.Fail(t, "Timed out")
	}
	fmt.Println("CanTimeOut Called:", d.CanTimeOutCalled)
	fmt.Println("TimedOut Called:", d.TimedOutCalled)
	fmt.Println("CheckResult Called:", d.CheckResultCalled)
}

func TestExclusiveGateway(t *testing.T) {
	tst := &integration{}
	tst.setup(t)
	defer tst.teardown()

	complete := make(chan *model.WorkflowInstanceComplete, 100)
	d := &testBoundaryTimerDef{
		CheckResultPause: time.Second * 3,
	}

	executeBoundaryTimerTest(t, complete, d)
	select {
	case c := <-complete:
		fmt.Println("completed " + c.WorkflowInstanceId)
	case <-time.After(20 * time.Second):
		assert.Fail(t, "Timed out")

	}
	fmt.Println("CanTimeOut Called:", d.CanTimeOutCalled)
	fmt.Println("TimedOut Called:", d.TimedOutCalled)
	fmt.Println("CheckResult Called:", d.CheckResultCalled)
}

func executeBoundaryTimerTest(t *testing.T, complete chan *model.WorkflowInstanceComplete, d *testBoundaryTimerDef) {

	// Create a starting context
	ctx := context.Background()

	// Create logger
	log, _ := zap.NewDevelopment()

	// Dial shar
	cl := client.New(log, client.WithEphemeralStorage())
	err := cl.Dial(natsURL)
	require.NoError(t, err)

	// Load BPMN workflow
	b, err := os.ReadFile("../testdata/possible-timeout-workflow.bpmn")
	require.NoError(t, err)

	_, err = cl.LoadBPMNWorkflowFromBytes(ctx, "PossibleTimeout", b)
	require.NoError(t, err)

	// Register a service task
	cl.RegisterWorkflowInstanceComplete(complete)
	err = cl.RegisterServiceTask(ctx, "CanTimeout", d.canTimeout)
	require.NoError(t, err)
	err = cl.RegisterServiceTask(ctx, "TimedOut", d.timedOut)
	require.NoError(t, err)
	err = cl.RegisterServiceTask(ctx, "CheckResult", d.checkResult)
	require.NoError(t, err)

	// Launch the workflow
	if _, err := cl.LaunchWorkflow(ctx, "PossibleTimeout", model.Vars{}); err != nil {
		panic(err)
	}

	// Listen for service tasks
	go func() {
		err := cl.Listen(ctx)
		require.NoError(t, err)
	}()
}

type testBoundaryTimerDef struct {
	mx                sync.Mutex
	CheckResultCalled int
	CanTimeOutCalled  int
	TimedOutCalled    int
	CanTimeOutPause   time.Duration
	CheckResultPause  time.Duration
}

func (d *testBoundaryTimerDef) canTimeout(ctx context.Context, vars model.Vars) (model.Vars, error) {
	fmt.Println("Can timeout")
	d.mx.Lock()
	d.CanTimeOutCalled++
	d.mx.Unlock()
	time.Sleep(d.CanTimeOutPause)
	return vars, nil
}

func (d *testBoundaryTimerDef) timedOut(ctx context.Context, vars model.Vars) (model.Vars, error) {
	fmt.Println("Timed out")
	d.mx.Lock()
	d.TimedOutCalled++
	d.mx.Unlock()
	return vars, nil
}

func (d *testBoundaryTimerDef) checkResult(ctx context.Context, vars model.Vars) (model.Vars, error) {
	fmt.Println("Check result")
	d.mx.Lock()
	d.CheckResultCalled++
	d.mx.Unlock()
	time.Sleep(d.CheckResultPause)
	return vars, nil
}
