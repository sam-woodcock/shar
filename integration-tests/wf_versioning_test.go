package intTests

import (
	"context"
	"github.com/nats-io/nats.go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gitlab.com/shar-workflow/shar/client"
	"gitlab.com/shar-workflow/shar/server/messages"
	"go.uber.org/zap"
	"os"
	"testing"
)

func TestWfVersioning(t *testing.T) {
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
	b, err := os.ReadFile("../testdata/simple-workflow.bpmn")
	require.NoError(t, err)

	_, err = cl.LoadBPMNWorkflowFromBytes(ctx, "SimpleWorkflowTest", b)
	require.NoError(t, err)
	res, err := cl.ListWorkflows(ctx)
	assert.Equal(t, int32(1), res[0].Version)
	_, err = cl.LoadBPMNWorkflowFromBytes(ctx, "SimpleWorkflowTest", b)
	require.NoError(t, err)
	res2, err := cl.ListWorkflows(ctx)
	assert.Equal(t, int32(1), res2[0].Version)
	nc, err := nats.Connect(natsURL)
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
