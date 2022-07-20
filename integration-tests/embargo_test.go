package integration_tests

import (
	"context"
	"fmt"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gitlab.com/shar-workflow/shar/client"
	"gitlab.com/shar-workflow/shar/model"
	"go.uber.org/zap"
	"os"
	"testing"
	"time"
)

func TestEmbargo(t *testing.T) {
	setup()
	defer teardown()
	// Create a starting context
	ctx := context.Background()

	// Create logger
	log, _ := zap.NewDevelopment()

	defer func() {
		if err := log.Sync(); err != nil {
		}
	}()

	// Dial shar
	cl := client.New(log, client.EphemeralStorage{})
	err := cl.Dial("nats://127.0.0.1:4459")
	require.NoError(t, err)

	// Load BPMN workflow
	b, err := os.ReadFile("../testdata/test-timer-parse-duration.bpmn")
	require.NoError(t, err)

	_, err = cl.LoadBPMNWorkflowFromBytes(ctx, "EmbargoTest", b)
	require.NoError(t, err)

	complete := make(chan *model.WorkflowInstanceComplete, 100)

	// Register a service task
	cl.RegisterWorkflowInstanceComplete(complete)

	sw := time.Now().UnixNano()
	// Launch the workflow
	if _, err := cl.LaunchWorkflow(ctx, "EmbargoTest", model.Vars{}); err != nil {
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
	}

	d := time.Duration(time.Now().UnixNano() - sw)
	assert.Equal(t, 2, int(d.Seconds()))
	// Check consistency
	//js, err := GetJetstream()

	/*	getKeys := func(kv string) ([]string, error) {
		messageSubs, err := js.KeyValue(kv)
		if err != nil {
			return nil, err
		}
		k, err := messageSubs.Keys()
		if err != nil {
			return nil, err
		}
		return k, nil
	}*/
}
