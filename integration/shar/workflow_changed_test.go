package intTest

import (
	"context"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gitlab.com/shar-workflow/shar/client"
	support "gitlab.com/shar-workflow/shar/integration-support"
	"os"
	"testing"
)

func TestWorkflowChanged(t *testing.T) {
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
	b, err := os.ReadFile("../../testdata/simple-workflow.bpmn")
	require.NoError(t, err)

	changed, err := cl.HasWorkflowDefinitionChanged(ctx, "SimpleWorkflowTest", b)
	require.NoError(t, err)
	assert.True(t, changed)

	_, err = cl.LoadBPMNWorkflowFromBytes(ctx, "SimpleWorkflowTest", b)
	require.NoError(t, err)

	changed, err = cl.HasWorkflowDefinitionChanged(ctx, "SimpleWorkflowTest", b)
	require.NoError(t, err)
	assert.False(t, changed)

	// Load second BPMN workflow
	b, err = os.ReadFile("../../testdata/simple-workflow-changed.bpmn")
	require.NoError(t, err)
	changed, err = cl.HasWorkflowDefinitionChanged(ctx, "SimpleWorkflowTest", b)
	require.NoError(t, err)
	assert.True(t, changed)
}
