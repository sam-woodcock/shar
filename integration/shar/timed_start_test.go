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
	tst.Setup(t, nil, nil)
	defer tst.Teardown()

	// Create a starting context
	ctx := context.Background()

	// Dial shar
	cl := client.New(client.WithEphemeralStorage(), client.WithConcurrency(10))
	err := cl.Dial(tst.NatsURL)
	require.NoError(t, err)

	// Load BPMN workflow
	b, err := os.ReadFile("../../testdata/timed-start-workflow.bpmn")
	require.NoError(t, err)

	_, err = cl.LoadBPMNWorkflowFromBytes(ctx, "TimedStartTest", b)
	require.NoError(t, err)

	d := &timedStartHandlerDef{tst: tst}

	// Register a service task
	err = cl.RegisterServiceTask(ctx, "SimpleProcess", d.integrationSimple)
	require.NoError(t, err)

	_, _, err = cl.LaunchWorkflow(ctx, "TimedStartTest", model.Vars{})
	require.NoError(t, err)

	// A hook to watch for completion
	complete := make(chan *model.WorkflowInstanceComplete, 100)
	cl.RegisterWorkflowInstanceComplete(complete)

	// Listen for service tasks
	go func() {
		err := cl.Listen(ctx)
		require.NoError(t, err)
	}()

	select {
	case <-time.After(30 * time.Second):
		assert.Fail(t, "timed out")
	case <-complete:
	}

	d.mx.Lock()
	defer d.mx.Unlock()
	assert.Equal(t, 32768, d.tst.FinalVars["carried"])
	assert.Equal(t, 3, d.count)
	fmt.Println("good")
	tst.AssertCleanKV()
}

type timedStartHandlerDef struct {
	mx    sync.Mutex
	count int
	tst   *support.Integration
}

func (d *timedStartHandlerDef) integrationSimple(_ context.Context, _ client.JobClient, vars model.Vars) (model.Vars, error) {
	fmt.Println("Hi")
	fmt.Println("carried", vars["carried"])
	d.mx.Lock()
	defer d.mx.Unlock()
	d.tst.FinalVars = vars
	d.count++
	return vars, nil
}
