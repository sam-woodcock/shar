package intTest

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gitlab.com/shar-workflow/shar/client"
	"gitlab.com/shar-workflow/shar/common/workflow"
	support "gitlab.com/shar-workflow/shar/integration-support"
	"gitlab.com/shar-workflow/shar/model"
)

func TestExclusiveGatewayDecision(t *testing.T) {
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
	b, err := os.ReadFile("../../testdata/exclusive-gateway.bpmn")
	require.NoError(t, err)

	_, err = cl.LoadBPMNWorkflowFromBytes(ctx, "ExclusiveGatewayTest", b)
	require.NoError(t, err)

	complete := make(chan *model.WorkflowInstanceComplete, 100)

	d := &testExclusiveGatewayDecisionDef{t: t, gameResult: "Win"}

	// Register a service task
	cl.RegisterWorkflowInstanceComplete(complete)
	err = cl.RegisterServiceTask(ctx, "PlayGame", d.playGame)
	require.NoError(t, err)
	err = cl.RegisterServiceTask(ctx, "ReceiveTrophy", d.win)
	require.NoError(t, err)
	err = cl.RegisterServiceTask(ctx, "ReceiveCommiserations", d.lose)
	require.NoError(t, err)

	// Launch the workflow
	if _, _, err := cl.LaunchWorkflow(ctx, "ExclusiveGatewayTest", model.Vars{"carried": 32768}); err != nil {
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
	tst.AssertCleanKV()
}

type testExclusiveGatewayDecisionDef struct {
	t          *testing.T
	gameResult string
}

func (d *testExclusiveGatewayDecisionDef) playGame(_ context.Context, _ client.JobClient, vars model.Vars) (model.Vars, workflow.WrappedError) {
	fmt.Println("Hi")
	assert.Equal(d.t, 32768, vars["carried"].(int))
	vars["GameResult"] = d.gameResult
	return vars, nil
}

func (d *testExclusiveGatewayDecisionDef) win(_ context.Context, _ client.JobClient, vars model.Vars) (model.Vars, workflow.WrappedError) {
	assert.Equal(d.t, "Win", vars["GameResult"].(string))
	assert.Equal(d.t, 32768, vars["carried"].(int))
	return vars, nil
}

func (d *testExclusiveGatewayDecisionDef) lose(_ context.Context, _ client.JobClient, vars model.Vars) (model.Vars, workflow.WrappedError) {
	assert.Equal(d.t, "Lose", vars["GameResult"].(string))
	assert.Equal(d.t, 32768, vars["carried"].(int))
	vars["Success"] = true
	return vars, nil
}
