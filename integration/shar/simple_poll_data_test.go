package intTest

import (
	"context"
	"fmt"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gitlab.com/shar-workflow/shar/client"
	dclient "gitlab.com/shar-workflow/shar/client/data"
	support "gitlab.com/shar-workflow/shar/integration-support"
	"gitlab.com/shar-workflow/shar/model"
	"os"
	"strings"
	"testing"
	"time"
)

func TestSimplePollData(t *testing.T) {
	tst := &support.Integration{}
	tst.Setup(t, nil, nil)
	defer tst.Teardown()

	// Create a starting context
	ctx := context.Background()

	// Dial shar
	cl := client.New(client.WithEphemeralStorage(), client.WithConcurrency(10))
	dcl := dclient.New(dclient.WithEphemeralStorage())
	err := cl.Dial(ctx, tst.NatsURL)
	require.NoError(t, err)
	err = dcl.Dial(ctx, tst.NatsURL)
	require.NoError(t, err)
	trace := make([]*model.WorkflowStateSummary, 0)
	go func() {
		for {
			ev, err := dcl.SpoolWorkflowEvents(ctx)
			if err != nil && strings.HasSuffix(err.Error(), "nats: Server Shutdown") {
				break
			}
			require.NoError(t, err)
			trace = append(trace, ev.State...)
		}
	}()

	// Load BPMN workflow
	b, err := os.ReadFile("../../testdata/simple-workflow.bpmn")
	require.NoError(t, err)

	_, err = cl.LoadBPMNWorkflowFromBytes(ctx, "SimpleWorkflowTest", b)
	require.NoError(t, err)

	d := &testSimplePollDataHandlerDef{t: t, finished: make(chan struct{})}

	// Register a service task
	err = cl.RegisterServiceTask(ctx, "SimpleProcess", d.integrationSimple)
	require.NoError(t, err)
	err = cl.RegisterProcessComplete("SimpleProcess", d.processEnd)
	require.NoError(t, err)

	// Launch the workflow
	_, _, err = cl.LaunchWorkflow(ctx, "SimpleWorkflowTest", model.Vars{})
	require.NoError(t, err)
	// Listen for service tasks
	go func() {
		err := cl.Listen(ctx)
		require.NoError(t, err)
	}()
	support.WaitForChan(t, d.finished, 20*time.Second)
	tst.AssertCleanKV()
	assert.Len(t, trace, 20)
}

type testSimplePollDataHandlerDef struct {
	t        *testing.T
	finished chan struct{}
}

func (d *testSimplePollDataHandlerDef) integrationSimple(_ context.Context, _ client.JobClient, vars model.Vars) (model.Vars, error) {
	fmt.Println("Hi")
	assert.Equal(d.t, 32768, vars["carried"].(int))
	assert.Equal(d.t, 42, vars["localVar"].(int))
	vars["Success"] = true
	return vars, nil
}

func (d *testSimplePollDataHandlerDef) processEnd(ctx context.Context, vars model.Vars, wfError *model.Error, state model.CancellationState) {
	close(d.finished)
}
