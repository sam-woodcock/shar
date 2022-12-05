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
	"testing"
	"time"
)

func TestEmbargo(t *testing.T) {
	tst := &support.Integration{}
	tst.Setup(t)
	defer tst.Teardown()

	// Create a starting context
	ctx := context.Background()

	// Dial shar
	cl := client.New(client.WithEphemeralStorage(), client.WithConcurrency(10))
	err := cl.Dial(support.NatsURL)
	require.NoError(t, err)

	// Load BPMN workflow
	b, err := os.ReadFile("../../testdata/test-timer-parse-duration.bpmn")
	require.NoError(t, err)

	_, err = cl.LoadBPMNWorkflowFromBytes(ctx, "TestEmbargo", b)
	require.NoError(t, err)

	complete := make(chan *model.WorkflowInstanceComplete, 100)

	// Register a service task
	cl.RegisterWorkflowInstanceComplete(complete)

	sw := time.Now().UnixNano()
	// Launch the workflow
	if _, err := cl.LaunchWorkflow(ctx, "TestEmbargo", model.Vars{}); err != nil {
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
	}

	d := time.Duration(time.Now().UnixNano() - sw)
	assert.Equal(t, 2, int(d.Seconds()))
	tst.AssertCleanKV()
}
