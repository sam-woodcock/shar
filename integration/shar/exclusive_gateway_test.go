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

func TestExclusiveGatewayDecision(t *testing.T) {
	tst := &support.Integration{}
	tst.Setup(t, nil, nil)
	defer tst.Teardown()

	// Create a starting context
	ctx := context.Background()

	// Dial shar
	cl := client.New(client.WithEphemeralStorage(), client.WithConcurrency(10))
	err := cl.Dial(ctx, tst.NatsURL)
	require.NoError(t, err)

	// Load BPMN workflow
	b, err := os.ReadFile("../../testdata/exclusive-gateway.bpmn")
	require.NoError(t, err)

	_, err = cl.LoadBPMNWorkflowFromBytes(ctx, "ExclusiveGatewayTest", b)
	require.NoError(t, err)

	d := &testExclusiveGatewayDecisionDef{t: t, gameResult: "Win", finished: make(chan struct{})}

	// Register a service task
	err = cl.RegisterServiceTask(ctx, "PlayGame", d.playGame)
	require.NoError(t, err)
	err = cl.RegisterServiceTask(ctx, "ReceiveTrophy", d.win)
	require.NoError(t, err)
	err = cl.RegisterServiceTask(ctx, "ReceiveCommiserations", d.lose)
	require.NoError(t, err)
	err = cl.RegisterProcessComplete("Process_1k2x28n", d.processEnd)
	require.NoError(t, err)

	// Launch the workflow
	_, _, err = cl.LaunchWorkflow(ctx, "ExclusiveGatewayTest", model.Vars{"carried": 32768})
	require.NoError(t, err)
	// Listen for service tasks
	go func() {
		err := cl.Listen(ctx)
		require.NoError(t, err)
	}()
	support.WaitForChan(t, d.finished, 20*time.Second)
	tst.AssertCleanKV()
}

type testExclusiveGatewayDecisionDef struct {
	t          *testing.T
	gameResult string
	finished   chan struct{}
}

func (d *testExclusiveGatewayDecisionDef) playGame(_ context.Context, _ client.JobClient, vars model.Vars) (model.Vars, error) {
	fmt.Println("Hi")
	assert.Equal(d.t, 32768, vars["carried"].(int))
	vars["GameResult"] = d.gameResult
	return vars, nil
}

func (d *testExclusiveGatewayDecisionDef) win(_ context.Context, _ client.JobClient, vars model.Vars) (model.Vars, error) {
	assert.Equal(d.t, "Win", vars["GameResult"].(string))
	assert.Equal(d.t, 32768, vars["carried"].(int))
	return vars, nil
}

func (d *testExclusiveGatewayDecisionDef) lose(_ context.Context, _ client.JobClient, vars model.Vars) (model.Vars, error) {
	assert.Equal(d.t, "Lose", vars["GameResult"].(string))
	assert.Equal(d.t, 32768, vars["carried"].(int))
	vars["Success"] = true
	return vars, nil
}

func (d *testExclusiveGatewayDecisionDef) processEnd(ctx context.Context, vars model.Vars, wfError *model.Error, state model.CancellationState) {
	close(d.finished)
}
