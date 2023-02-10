package intTest

import (
	"bytes"
	"context"
	"fmt"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gitlab.com/shar-workflow/shar/client"
	"gitlab.com/shar-workflow/shar/client/parser"
	"gitlab.com/shar-workflow/shar/common"
	support "gitlab.com/shar-workflow/shar/integration-support"
	"gitlab.com/shar-workflow/shar/model"
	"os"
	"testing"
)

func TestExclusiveParse(t *testing.T) {

	// Load BPMN workflow
	b, err := os.ReadFile("../../testdata/gateway-exclusive-out-and-in-test.bpmn")
	require.NoError(t, err)

	wf, err := parser.Parse("SimpleWorkflowTest", bytes.NewBuffer(b))
	require.NoError(t, err)

	els := make(map[string]*model.Element)
	common.IndexProcessElements(wf.Process["Process_0ljss15"].Elements, els)
	assert.Equal(t, model.GatewayType_exclusive, els["Gateway_01xjq2a"].Gateway.Type)
	assert.Equal(t, model.GatewayDirection_divergent, els["Gateway_01xjq2a"].Gateway.Direction)
	assert.Equal(t, model.GatewayType_exclusive, els["Gateway_1ps8xyt"].Gateway.Type)
	assert.Equal(t, model.GatewayDirection_convergent, els["Gateway_1ps8xyt"].Gateway.Direction)
	assert.Equal(t, "Gateway_01xjq2a", els["Gateway_1ps8xyt"].Gateway.ReciprocalId)
	assert.Equal(t, "Gateway_1ps8xyt", els["Gateway_01xjq2a"].Gateway.ReciprocalId)
}

func TestNestedExclusiveParse(t *testing.T) {

	// Load BPMN workflow
	b, err := os.ReadFile("../../testdata/gateway-multi-exclusive-out-and-in-test.bpmn")
	require.NoError(t, err)

	wf, err := parser.Parse("SimpleWorkflowTest", bytes.NewBuffer(b))
	require.NoError(t, err)

	els := make(map[string]*model.Element)
	common.IndexProcessElements(wf.Process["Process_0ljss15"].Elements, els)
	assert.Equal(t, "Gateway_0bcqcrc", els["Gateway_1ucd1b5"].Gateway.ReciprocalId)
	assert.Equal(t, "Gateway_1ps8xyt", els["Gateway_01xjq2a"].Gateway.ReciprocalId)
}

func TestExclusiveRun(t *testing.T) {
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
	b, err := os.ReadFile("../../testdata/gateway-exclusive-out-and-in-test.bpmn")
	require.NoError(t, err)

	_, err = cl.LoadBPMNWorkflowFromBytes(ctx, "ExclusiveGatewayTest", b)
	require.NoError(t, err)

	err = cl.RegisterServiceTask(ctx, "stage1", stage1)
	require.NoError(t, err)
	err = cl.RegisterServiceTask(ctx, "stage2", stage2)
	require.NoError(t, err)
	err = cl.RegisterServiceTask(ctx, "stage3", stage3)
	require.NoError(t, err)
	complete := make(chan *model.WorkflowInstanceComplete, 100)

	// Register a service task
	cl.RegisterWorkflowInstanceComplete(complete)

	// Launch the workflow
	wfiID, _, err := cl.LaunchWorkflow(ctx, "ExclusiveGatewayTest", model.Vars{"carried": 32768})
	require.NoError(t, err)

	// Listen for service tasks
	go func() {
		err := cl.Listen(ctx)
		require.NoError(t, err)
	}()
	tst.AwaitWorkflowComplete(t, complete, wfiID)
	tst.AssertCleanKV()

}

func stage3(ctx context.Context, jobClient client.JobClient, vars model.Vars) (model.Vars, error) {
	fmt.Println("Stage 3")
	return vars, nil
}

func stage2(ctx context.Context, jobClient client.JobClient, vars model.Vars) (model.Vars, error) {
	fmt.Println("Stage 2")
	return vars, nil
}

func stage1(ctx context.Context, jobClient client.JobClient, vars model.Vars) (model.Vars, error) {
	fmt.Println("Stage 1")
	return vars, nil
}
