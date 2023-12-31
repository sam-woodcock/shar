package intTest

import (
	"context"
	"fmt"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"gitlab.com/shar-workflow/shar/client"
	support "gitlab.com/shar-workflow/shar/integration-support"
	"gitlab.com/shar-workflow/shar/model"
	"go.opentelemetry.io/otel/sdk/trace"
	"golang.org/x/exp/slog"
	"os"
	"testing"
	"time"
)

func TestSimpleTelemetry(t *testing.T) {
	tel := &MockTelemetry{}
	tst := &support.Integration{WithTelemetry: tel}
	tst.Setup(t, nil, nil)
	defer tst.Teardown()

	tel.On("ExportSpans", mock.AnythingOfType("*context.valueCtx"), mock.AnythingOfType("[]trace.ReadOnlySpan")).
		Run(func(args mock.Arguments) {
			sp := args.Get(1).([]trace.ReadOnlySpan)
			slog.Debug(fmt.Sprintf("%v", sp[0].Name()))
		}).
		Return(nil).Times(5)

	// Create a starting context
	ctx := context.Background()

	// Dial shar
	cl := client.New(client.WithEphemeralStorage(), client.WithConcurrency(10))
	err := cl.Dial(ctx, tst.NatsURL)
	require.NoError(t, err)

	// Load BPMN workflow
	b, err := os.ReadFile("../../testdata/simple-workflow.bpmn")
	require.NoError(t, err)

	_, err = cl.LoadBPMNWorkflowFromBytes(ctx, "SimpleWorkflowTest", b)
	require.NoError(t, err)

	d := &testTelSimpleHandlerDef{t: t, finished: make(chan struct{})}

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
	tel.AssertExpectations(t)
	tst.AssertCleanKV()
}

type testTelSimpleHandlerDef struct {
	t        *testing.T
	finished chan struct{}
}

func (d *testTelSimpleHandlerDef) integrationSimple(_ context.Context, _ client.JobClient, vars model.Vars) (model.Vars, error) {
	fmt.Println("Hi")
	assert.Equal(d.t, 32768, vars["carried"].(int))
	vars["Success"] = true
	return vars, nil
}

func (d *testTelSimpleHandlerDef) processEnd(ctx context.Context, vars model.Vars, wfError *model.Error, state model.CancellationState) {
	close(d.finished)
}
