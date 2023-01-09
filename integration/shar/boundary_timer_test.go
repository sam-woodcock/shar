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

func TestBoundaryTimer(t *testing.T) {
	tst := &support.Integration{}
	tst.Setup(t, nil, nil)
	defer tst.Teardown()

	complete := make(chan *model.WorkflowInstanceComplete, 100)
	d := &testBoundaryTimerDef{
		tst: tst,
	}

	executeBoundaryTimerTest(t, complete, d)
	select {
	case c := <-complete:
		fmt.Println("completed " + c.WorkflowInstanceId)
	case <-time.After(20 * time.Second):
		require.Fail(t, "Timed out")
	}
	fmt.Println("CanTimeOut Called:", d.CanTimeOutCalled)
	fmt.Println("NoTimeout Called:", d.NoTimeoutCalled)
	fmt.Println("TimedOut Called:", d.TimedOutCalled)
	fmt.Println("CheckResult Called:", d.CheckResultCalled)
	tst.AssertCleanKV()
}

func TestBoundaryTimerTimeout(t *testing.T) {
	tst := &support.Integration{}
	tst.Setup(t, nil, nil)
	defer tst.Teardown()

	//sub := tracer.Trace("nats://127.0.0.1:4459")
	//defer sub.Drain()

	complete := make(chan *model.WorkflowInstanceComplete, 100)
	d := &testBoundaryTimerDef{
		CanTimeOutPause:  time.Second * 5,
		CheckResultPause: time.Second * 4,
		tst:              tst,
	}

	executeBoundaryTimerTest(t, complete, d)
	select {
	case c := <-complete:
		fmt.Println("completed " + c.WorkflowInstanceId)
	case <-time.After(20 * time.Second):
		require.Fail(t, "Timed out")
	}
	fmt.Println("CanTimeOut Called:", d.CanTimeOutCalled)
	fmt.Println("NoTimeout Called:", d.NoTimeoutCalled)
	fmt.Println("TimedOut Called:", d.TimedOutCalled)
	fmt.Println("CheckResult Called:", d.CheckResultCalled)
	tst.AssertCleanKV()
}

func TestExclusiveGateway(t *testing.T) {
	tst := &support.Integration{}
	tst.Setup(t, nil, nil)
	defer tst.Teardown()

	complete := make(chan *model.WorkflowInstanceComplete, 100)
	d := &testBoundaryTimerDef{
		CheckResultPause: time.Second * 3,
		tst:              tst,
	}

	executeBoundaryTimerTest(t, complete, d)
	select {
	case c := <-complete:
		fmt.Println("completed " + c.WorkflowInstanceId)
	case <-time.After(5 * time.Second):
		require.Fail(t, "Timed out")

	}
	fmt.Println("CanTimeOut Called:", d.CanTimeOutCalled)
	fmt.Println("NoTimeout Called:", d.NoTimeoutCalled)
	fmt.Println("TimedOut Called:", d.TimedOutCalled)
	fmt.Println("CheckResult Called:", d.CheckResultCalled)
	tst.AssertCleanKV()
}

func executeBoundaryTimerTest(t *testing.T, complete chan *model.WorkflowInstanceComplete, d *testBoundaryTimerDef) {

	// Create a starting context
	ctx := context.Background()

	// Dial shar
	cl := client.New(client.WithEphemeralStorage(), client.WithConcurrency(10))
	err := cl.Dial(d.tst.NatsURL)
	require.NoError(t, err)

	// Load BPMN workflow
	b, err := os.ReadFile("../../testdata/possible-timeout-workflow.bpmn")
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
	err = cl.RegisterServiceTask(ctx, "NoTimeout", d.noTimeout)
	require.NoError(t, err)

	// Launch the workflow
	if _, _, err := cl.LaunchWorkflow(ctx, "PossibleTimeout", model.Vars{}); err != nil {
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
	NoTimeoutCalled   int
	CanTimeOutPause   time.Duration
	CheckResultPause  time.Duration
	NoTimeoutPause    time.Duration
	tst               *support.Integration
}

func (d *testBoundaryTimerDef) canTimeout(_ context.Context, _ client.JobClient, vars model.Vars) (model.Vars, workflow.WrappedError) {
	d.mx.Lock()
	d.CanTimeOutCalled++
	d.mx.Unlock()
	time.Sleep(d.CanTimeOutPause)
	return vars, nil
}

func (d *testBoundaryTimerDef) noTimeout(_ context.Context, _ client.JobClient, vars model.Vars) (model.Vars, workflow.WrappedError) {
	d.mx.Lock()
	d.NoTimeoutCalled++
	d.mx.Unlock()
	time.Sleep(d.NoTimeoutPause)
	return vars, nil
}

func (d *testBoundaryTimerDef) timedOut(_ context.Context, _ client.JobClient, vars model.Vars) (model.Vars, workflow.WrappedError) {
	d.mx.Lock()
	d.TimedOutCalled++
	d.mx.Unlock()
	return vars, nil
}

func (d *testBoundaryTimerDef) checkResult(_ context.Context, _ client.JobClient, vars model.Vars) (model.Vars, workflow.WrappedError) {
	d.mx.Lock()
	d.CheckResultCalled++
	d.mx.Unlock()
	time.Sleep(d.CheckResultPause)
	return vars, nil
}
