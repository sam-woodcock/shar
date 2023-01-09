package intTest

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"gitlab.com/shar-workflow/shar/client"
	"gitlab.com/shar-workflow/shar/common/workflow"
	support "gitlab.com/shar-workflow/shar/integration-support"
	"gitlab.com/shar-workflow/shar/model"
	"go.opentelemetry.io/otel/sdk/trace"
	"golang.org/x/exp/slog"
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
	err := cl.Dial(tst.NatsURL)
	require.NoError(t, err)

	// Load BPMN workflow
	b, err := os.ReadFile("../../testdata/simple-workflow.bpmn")
	require.NoError(t, err)

	_, err = cl.LoadBPMNWorkflowFromBytes(ctx, "SimpleWorkflowTest", b)
	require.NoError(t, err)

	complete := make(chan *model.WorkflowInstanceComplete, 100)

	d := &testTelSimpleHandlerDef{t: t}

	// Register a service task
	cl.RegisterWorkflowInstanceComplete(complete)
	err = cl.RegisterServiceTask(ctx, "SimpleProcess", d.integrationSimple)
	require.NoError(t, err)

	// Launch the workflow
	if _, _, err := cl.LaunchWorkflow(ctx, "SimpleWorkflowTest", model.Vars{}); err != nil {
		panic(err)
	}

	// Listen for service tasks
	go func() {
		err := cl.Listen(ctx)
		require.NoError(t, err)
	}()
	select {
	case c := <-complete:
		fmt.Println("completed " + c.WorkflowInstanceId)
	case <-time.After(5 * time.Second):
		require.Fail(t, "Timed out")
	}
	tel.AssertExpectations(t)
	tst.AssertCleanKV()
}

type testTelSimpleHandlerDef struct {
	t *testing.T
}

func (d *testTelSimpleHandlerDef) integrationSimple(_ context.Context, _ client.JobClient, vars model.Vars) (model.Vars, workflow.WrappedError) {
	fmt.Println("Hi")
	assert.Equal(d.t, 32768, vars["carried"].(int))
	vars["Success"] = true
	return vars, nil
}
