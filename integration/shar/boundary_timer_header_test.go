package intTest

import (
	"context"
	"fmt"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gitlab.com/shar-workflow/shar/client"
	"gitlab.com/shar-workflow/shar/common/header"
	support "gitlab.com/shar-workflow/shar/integration-support"
	"gitlab.com/shar-workflow/shar/model"
	"os"
	"sync"
	"testing"
	"time"
)

func TestBoundaryTimerHeaders(t *testing.T) {
	tst := &support.Integration{}
	tst.Setup(t, nil, nil)
	defer tst.Teardown()
	complete := make(chan *model.WorkflowInstanceComplete, 100)
	d := &testBoundaryTimerHeaderDef{tst: tst, finished: make(chan struct{})}

	executeBoundaryTimerHeaderTest(t, complete, d)
	support.WaitForChan(t, d.finished, 20*time.Second)
	fmt.Println("CanTimeOut Called:", d.CanTimeOutCalled)
	fmt.Println("NoTimeout Called:", d.NoTimeoutCalled)
	fmt.Println("TimedOut Called:", d.TimedOutCalled)
	fmt.Println("CheckResult Called:", d.CheckResultCalled)
	tst.AssertCleanKV()
}

func TestBoundaryTimerTimeoutHeaders(t *testing.T) {
	tst := &support.Integration{}
	tst.WithTrace = true
	tst.Setup(t, nil, nil)
	defer tst.Teardown()

	complete := make(chan *model.WorkflowInstanceComplete, 100)
	d := &testBoundaryTimerHeaderDef{
		CanTimeOutPause:  time.Second * 5,
		CheckResultPause: time.Second * 4,
		tst:              tst,
		finished:         make(chan struct{}),
	}

	executeBoundaryTimerHeaderTest(t, complete, d)
	support.WaitForChan(t, d.finished, 20*time.Second)
	fmt.Println("CanTimeOut Called:", d.CanTimeOutCalled)
	fmt.Println("NoTimeout Called:", d.NoTimeoutCalled)
	fmt.Println("TimedOut Called:", d.TimedOutCalled)
	fmt.Println("CheckResult Called:", d.CheckResultCalled)
	tst.AssertCleanKV()
}

func TestExclusiveGatewayHeaders(t *testing.T) {
	tst := &support.Integration{}
	tst.Setup(t, nil, nil)
	defer tst.Teardown()

	complete := make(chan *model.WorkflowInstanceComplete, 100)
	d := &testBoundaryTimerHeaderDef{
		CheckResultPause: time.Second * 3,
		tst:              tst,
		finished:         make(chan struct{}),
	}

	executeBoundaryTimerHeaderTest(t, complete, d)
	support.WaitForChan(t, d.finished, 20*time.Second)
	fmt.Println("CanTimeOut Called:", d.CanTimeOutCalled)
	fmt.Println("NoTimeout Called:", d.NoTimeoutCalled)
	fmt.Println("TimedOut Called:", d.TimedOutCalled)
	fmt.Println("CheckResult Called:", d.CheckResultCalled)
	tst.AssertCleanKV()
}

func executeBoundaryTimerHeaderTest(t *testing.T, complete chan *model.WorkflowInstanceComplete, d *testBoundaryTimerHeaderDef) {
	d.t = t
	// Create a starting context
	ctx := context.Background()
	ctx = header.Set(ctx, "sample", "ok")
	// Dial shar
	cl := client.New(client.WithEphemeralStorage(), client.WithConcurrency(10))
	err := cl.Dial(ctx, d.tst.NatsURL)
	require.NoError(t, err)

	// Load BPMN workflow
	b, err := os.ReadFile("../../testdata/possible-timeout-workflow.bpmn")
	require.NoError(t, err)

	_, err = cl.LoadBPMNWorkflowFromBytes(ctx, "PossibleTimeout", b)
	require.NoError(t, err)

	// Register a service task
	err = cl.RegisterServiceTask(ctx, "CanTimeout", d.canTimeout)
	require.NoError(t, err)
	err = cl.RegisterServiceTask(ctx, "TimedOut", d.timedOut)
	require.NoError(t, err)
	err = cl.RegisterServiceTask(ctx, "CheckResult", d.checkResult)
	require.NoError(t, err)
	err = cl.RegisterServiceTask(ctx, "NoTimeout", d.noTimeout)
	require.NoError(t, err)
	err = cl.RegisterProcessComplete("Process_16piog5", d.processEnd)
	require.NoError(t, err)

	// Launch the workflow
	_, _, err = cl.LaunchWorkflow(ctx, "PossibleTimeout", model.Vars{})
	require.NoError(t, err)
	// Listen for service tasks
	go func() {
		err := cl.Listen(ctx)
		require.NoError(t, err)
	}()
}

type testBoundaryTimerHeaderDef struct {
	mx                sync.Mutex
	CheckResultCalled int
	CanTimeOutCalled  int
	TimedOutCalled    int
	NoTimeoutCalled   int
	CanTimeOutPause   time.Duration
	CheckResultPause  time.Duration
	NoTimeoutPause    time.Duration
	t                 *testing.T
	tst               *support.Integration
	finished          chan struct{}
}

func (d *testBoundaryTimerHeaderDef) canTimeout(ctx context.Context, _ client.JobClient, vars model.Vars) (model.Vars, error) {
	assert.Equal(d.t, "ok", header.Get(ctx, "sample"))
	d.mx.Lock()
	d.CanTimeOutCalled++
	d.mx.Unlock()
	time.Sleep(d.CanTimeOutPause)
	return vars, nil
}

func (d *testBoundaryTimerHeaderDef) noTimeout(ctx context.Context, _ client.JobClient, vars model.Vars) (model.Vars, error) {
	assert.Equal(d.t, "ok", header.Get(ctx, "sample"))
	d.mx.Lock()
	d.NoTimeoutCalled++
	d.mx.Unlock()
	time.Sleep(d.NoTimeoutPause)
	return vars, nil
}

func (d *testBoundaryTimerHeaderDef) timedOut(ctx context.Context, _ client.JobClient, vars model.Vars) (model.Vars, error) {
	assert.Equal(d.t, "ok", header.Get(ctx, "sample"))
	d.mx.Lock()
	d.TimedOutCalled++
	d.mx.Unlock()
	return vars, nil
}

func (d *testBoundaryTimerHeaderDef) checkResult(ctx context.Context, _ client.JobClient, vars model.Vars) (model.Vars, error) {
	assert.Equal(d.t, "ok", header.Get(ctx, "sample"))
	d.mx.Lock()
	d.CheckResultCalled++
	d.mx.Unlock()
	time.Sleep(d.CheckResultPause)
	return vars, nil
}

func (d *testBoundaryTimerHeaderDef) processEnd(ctx context.Context, vars model.Vars, wfError *model.Error, state model.CancellationState) {
	d.finished <- struct{}{}
}
