package intTest

import (
	"bytes"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gitlab.com/shar-workflow/shar/client/parser"
	"gitlab.com/shar-workflow/shar/common"
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
