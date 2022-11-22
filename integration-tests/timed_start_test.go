package intTests

import (
	"context"
	"fmt"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gitlab.com/shar-workflow/shar/client"
	"gitlab.com/shar-workflow/shar/model"
	"os"
	"sync"
	"testing"
	"time"
)

func TestTimedStart(t *testing.T) {
	tst := &integration{}
	tst.setup(t)
	defer tst.teardown()

	// Create a starting context
	ctx := context.Background()

	// Dial shar
	cl := client.New(client.WithEphemeralStorage())
	err := cl.Dial(natsURL)
	require.NoError(t, err)

	// Load BPMN workflow
	b, err := os.ReadFile("../testdata/timed-start-workflow.bpmn")
	require.NoError(t, err)

	_, err = cl.LoadBPMNWorkflowFromBytes(ctx, "TimedStartTest", b)
	require.NoError(t, err)

	d := &timedStartHandlerDef{tst: tst}

	// Register a service task
	err = cl.RegisterServiceTask(ctx, "SimpleProcess", d.integrationSimple)
	require.NoError(t, err)

	// Launch the workflow
	if _, err := cl.LaunchWorkflow(ctx, "TimedStartTest", model.Vars{"carried2": "carried2value"}); err != nil {
		panic(err)
	}

	// Listen for service tasks
	go func() {
		err := cl.Listen(ctx)
		require.NoError(t, err)
	}()

	time.Sleep(5 * time.Second)
	d.mx.Lock()
	defer d.mx.Unlock()
	assert.Equal(t, 32768, d.tst.finalVars["carried"])
	assert.Equal(t, 3, d.count)
	tst.AssertCleanKV()
}

type timedStartHandlerDef struct {
	mx    sync.Mutex
	count int
	tst   *integration
}

func (d *timedStartHandlerDef) integrationSimple(_ context.Context, _ client.JobClient, vars model.Vars) (model.Vars, error) {
	fmt.Println("Hi")
	fmt.Println("carried", vars["carried"])
	d.mx.Lock()
	defer d.mx.Unlock()
	d.tst.finalVars = vars
	d.count++
	return vars, nil
}
