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

	// Launch the workflow
	if _, _, err := cl.LaunchWorkflow(ctx, "TimedStartTest", model.Vars{"carried2": "carried2value"}); err != nil {
		panic(err)
	}

	// Listen for service tasks
	go func() {
		err := cl.Listen(ctx)
		require.NoError(t, err)
	}()

	time.Sleep(10 * time.Second)
	d.mx.Lock()
	defer d.mx.Unlock()
	assert.Equal(t, 32768, d.tst.FinalVars["carried"])
	assert.Equal(t, 3, d.count)
	tst.AssertCleanKV()
}

type timedStartHandlerDef struct {
	mx    sync.Mutex
	count int
	tst   *support.Integration
}

func (d *timedStartHandlerDef) integrationSimple(_ context.Context, _ client.JobClient, vars model.Vars) (model.Vars, workflow.WrappedError) {
	fmt.Println("Hi")
	fmt.Println("carried", vars["carried"])
	d.mx.Lock()
	defer d.mx.Unlock()
	d.tst.FinalVars = vars
	d.count++
	return vars, nil
}
