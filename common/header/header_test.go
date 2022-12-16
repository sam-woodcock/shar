package header

import (
	"context"
	"github.com/nats-io/nats.go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"testing"
)

func TestContextEncodimg(t *testing.T) {
	ctx := context.Background()
	ctx2 := ToCtx(ctx, Values{"name1": "value1"})
	val := FromCtx(ctx2)
	assert.Equal(t, "value1", val["name1"])
}

func TestMsgEncoding(t *testing.T) {
	ctx := context.Background()
	msg := nats.NewMsg("testSubject")
	err := ToMsg(ctx, Values{"name1": "value1"}, msg)
	require.NoError(t, err)
	val, err := FromMsg(ctx, msg)
	require.NoError(t, err)
	assert.Equal(t, "value1", val["name1"])
}

func TestMsgNilDecoding(t *testing.T) {
	ctx := context.Background()
	msg := nats.NewMsg("testSubject")
	val, err := FromMsg(ctx, msg)
	require.NoError(t, err)
	assert.Equal(t, "", val["name1"])
}
func TestContextNilDecodimg(t *testing.T) {
	ctx := context.Background()
	val := FromCtx(ctx)
	assert.Equal(t, "", val["name1"])
}
