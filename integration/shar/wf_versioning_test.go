package intTest

import (
	"context"
	"github.com/nats-io/nats.go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gitlab.com/shar-workflow/shar/client"
	"gitlab.com/shar-workflow/shar/server/messages"

	"os"
	"testing"
)

func TestWfVersioning(t *testing.T) {
	tst := &Integration{}
	tst.Setup(t)
	defer tst.Teardown()

	// Create a starting context
	ctx := context.Background()

	// Dial shar
	cl := client.New(client.WithEphemeralStorage(), client.WithConcurrency(10))
	err := cl.Dial(NatsURL)
	require.NoError(t, err)

	// Load BPMN workflow
	b, err := os.ReadFile("../../testdata/simple-workflow.bpmn")
	require.NoError(t, err)

	_, err = cl.LoadBPMNWorkflowFromBytes(ctx, "SimpleWorkflowTest", b)
	require.NoError(t, err)
	res, err := cl.ListWorkflows(ctx)
	require.NoError(t, err)
	assert.Equal(t, int32(1), res[0].Version)
	_, err = cl.LoadBPMNWorkflowFromBytes(ctx, "SimpleWorkflowTest", b)
	require.NoError(t, err)
	res2, err := cl.ListWorkflows(ctx)
	require.NoError(t, err)
	assert.Equal(t, int32(1), res2[0].Version)
	nc, err := nats.Connect(NatsURL)
	require.NoError(t, err)
	js, err := nc.JetStream()
	require.NoError(t, err)
	kv, err := js.KeyValue(messages.KvDefinition)
	require.NoError(t, err)
	keys, err := kv.Keys()
	require.NoError(t, err)
	assert.Equal(t, 1, len(keys))
	tst.AssertCleanKV()
}
