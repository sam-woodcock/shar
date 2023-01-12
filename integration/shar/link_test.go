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

func TestLink(t *testing.T) {
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
	b, err := os.ReadFile("../../testdata/link.bpmn")
	require.NoError(t, err)

	_, err = cl.LoadBPMNWorkflowFromBytes(ctx, "LinkTest", b)
	require.NoError(t, err)

	complete := make(chan *model.WorkflowInstanceComplete, 100)

	d := &testLinkHandlerDef{t: t}

	// Register a service task
	cl.RegisterWorkflowInstanceComplete(complete)
	err = cl.RegisterServiceTask(ctx, "spillage", d.spillage)
	require.NoError(t, err)
	err = cl.RegisterServiceTask(ctx, "dontCry", d.dontCry)
	require.NoError(t, err)
	err = cl.RegisterServiceTask(ctx, "cry", d.cry)
	require.NoError(t, err)
	err = cl.RegisterServiceTask(ctx, "wipeItUp", d.wipeItUp)
	require.NoError(t, err)

	// Launch the workflow
	if _, _, err := cl.LaunchWorkflow(ctx, "LinkTest", model.Vars{}); err != nil {
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
	assert.True(t, d.hitEnd)
	assert.True(t, d.hitResponse)
	tst.AssertCleanKV()
}

type testLinkHandlerDef struct {
	t           *testing.T
	mx          sync.Mutex
	hitEnd      bool
	hitResponse bool
}

func (d *testLinkHandlerDef) spillage(_ context.Context, _ client.JobClient, vars model.Vars) (model.Vars, error) {
	fmt.Println("Spilled")
	vars["substance"] = "beer"
	return vars, nil
}

func (d *testLinkHandlerDef) dontCry(_ context.Context, _ client.JobClient, vars model.Vars) (model.Vars, error) {
	fmt.Println("No tears shed")
	d.mx.Lock()
	defer d.mx.Unlock()
	d.hitResponse = true
	return vars, nil
}

func (d *testLinkHandlerDef) cry(_ context.Context, _ client.JobClient, vars model.Vars) (model.Vars, error) {
	fmt.Println("*sob*")
	d.mx.Lock()
	defer d.mx.Unlock()
	d.hitResponse = true
	return vars, nil
}

func (d *testLinkHandlerDef) wipeItUp(_ context.Context, _ client.JobClient, vars model.Vars) (model.Vars, error) {
	fmt.Println("all mopped up")
	d.mx.Lock()
	defer d.mx.Unlock()
	d.hitEnd = true
	return vars, nil
}
