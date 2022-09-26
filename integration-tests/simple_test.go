package main

import (
	"context"
	"fmt"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gitlab.com/shar-workflow/shar/client"
	"gitlab.com/shar-workflow/shar/model"
	"gitlab.com/shar-workflow/shar/server/tools/tracer"
	"go.uber.org/zap"
	"os"
	"testing"
	"time"
)

func TestSimple(t *testing.T) {
	//	if os.Getenv("INT_TEST") != "true" {
	//		t.Skip("Skipping integration test " + t.Name())
	//	}

	tst := &integration{}
	tst.setup(t)
	defer tst.teardown()

	sub := tracer.Trace("nats://127.0.0.1:4459")
	defer sub.Drain()

	// Create a starting context
	ctx := context.Background()

	// Create logger
	log, _ := zap.NewDevelopment()

	// Dial shar
	cl := client.New(log, client.WithEphemeralStorage())
	err := cl.Dial(natsURL)
	require.NoError(t, err)

	// Load BPMN workflow
	b, err := os.ReadFile("../testdata/simple-workflow.bpmn")
	require.NoError(t, err)

	_, err = cl.LoadBPMNWorkflowFromBytes(ctx, "SimpleWorkflowTest", b)
	require.NoError(t, err)

	complete := make(chan *model.WorkflowInstanceComplete, 100)

	d := &testSimpleHandlerDef{}

	// Register a service task
	cl.RegisterWorkflowInstanceComplete(complete)
	err = cl.RegisterServiceTask(ctx, "SimpleProcess", d.integrationSimple)
	require.NoError(t, err)

	// Launch the workflow
	if _, err := cl.LaunchWorkflow(ctx, "SimpleWorkflowTest", model.Vars{}); err != nil {
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
	case <-time.After(20 * time.Second):
		assert.Fail(t, "Timed out")
	}

}

type testSimpleHandlerDef struct {
}

func (d *testSimpleHandlerDef) integrationSimple(ctx context.Context, vars model.Vars) (model.Vars, error) {
	fmt.Println("Hi")
	return vars, nil
}
