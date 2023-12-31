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

func TestTimedStart(t *testing.T) {
	tst := &support.Integration{}
	//tst.WithTrace = true
	tst.Setup(t, nil, nil)
	defer tst.Teardown()

	// Create a starting context
	ctx := context.Background()

	// Dial shar
	cl := client.New(client.WithEphemeralStorage(), client.WithConcurrency(10))
	err := cl.Dial(ctx, tst.NatsURL)
	require.NoError(t, err)

	// Load BPMN workflow
	b, err := os.ReadFile("../../testdata/timed-start-workflow.bpmn")
	require.NoError(t, err)

	_, err = cl.LoadBPMNWorkflowFromBytes(ctx, "TimedStartTest", b)
	require.NoError(t, err)

	d := &timedStartHandlerDef{tst: tst, t: t, finished: make(chan struct{})}

	// Register a service task
	err = cl.RegisterServiceTask(ctx, "SimpleProcess", d.integrationSimple)
	require.NoError(t, err)

	_, _, err = cl.LaunchWorkflow(ctx, "TimedStartTest", model.Vars{})
	require.NoError(t, err)

	// A hook to watch for completion
	err = cl.RegisterProcessComplete("Process_1hikszy", d.processEnd)
	require.NoError(t, err)

	// Listen for service tasks
	go func() {
		err := cl.Listen(ctx)
		require.NoError(t, err)
	}()

	for i := 1; i <= 3; i++ {
		support.WaitForChan(t, d.finished, 20*time.Second)
	}
	d.mx.Lock()
	defer d.mx.Unlock()
	assert.Equal(t, 32768, d.tst.FinalVars["carried"])
	assert.Equal(t, 3, d.count)
	fmt.Println("good")
	tst.AssertCleanKV()
}

type timedStartHandlerDef struct {
	mx       sync.Mutex
	count    int
	tst      *support.Integration
	t        *testing.T
	finished chan struct{}
}

func (d *timedStartHandlerDef) integrationSimple(_ context.Context, _ client.JobClient, vars model.Vars) (model.Vars, error) {
	// TODO: Include for diagnosing timed start bug
	//assert.Equal(d.t, 32768, vars["carried"])
	d.mx.Lock()
	defer d.mx.Unlock()
	d.tst.FinalVars = vars
	d.count++
	return vars, nil
}

func (d *timedStartHandlerDef) processEnd(ctx context.Context, vars model.Vars, wfError *model.Error, state model.CancellationState) {
	d.finished <- struct{}{}
}
