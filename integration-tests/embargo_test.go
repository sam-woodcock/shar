package intTests

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
	tst := &integration{}
	tst.setup(t)
	defer tst.teardown()

	// Create a starting context
	ctx := context.Background()

	// Create logger
	log, _ := zap.NewDevelopment()

	// Dial shar
	cl := client.New(log, client.WithEphemeralStorage())
	err := cl.Dial(natsURL)
	require.NoError(t, err)

	// Load BPMN workflow
	b, err := os.ReadFile("../testdata/test-timer-parse-duration.bpmn")
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
