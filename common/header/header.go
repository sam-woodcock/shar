package header

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/gob"
	"fmt"
	"github.com/nats-io/nats.go"
)

// Values is a container for SHAR header values
type Values map[string]string

const natsSharHeader = "Shar-Header"

type contextKey string

var headerContextValue contextKey = "SHARHeader"

// FromCtx extracts headers from a context
func FromCtx(ctx context.Context) (v Values) {
	defer func() {
		if r := recover(); r != nil {
			v = make(Values)
		}
	}()
	return ctx.Value(headerContextValue).(Values)
}

// ToCtx creates a child context containing headers
func ToCtx(ctx context.Context, values Values) context.Context {
	return context.WithValue(ctx, headerContextValue, values)
}

// FromMsg extracts SHAR headers from a NATS message
func FromMsg(ctx context.Context, msg *nats.Msg) (Values, error) {
	hdr := msg.Header.Get(natsSharHeader)
	bin, err := base64.StdEncoding.DecodeString(hdr)
	if err != nil {
		return nil, fmt.Errorf("failed to decode base 64 header: %w", err)
	}
	if len(bin) == 0 {
		return make(Values), nil
	}
	dec := gob.NewDecoder(bytes.NewBuffer(bin))
	m := make(Values)
	err = dec.Decode(&m)
	if err != nil {
		return nil, fmt.Errorf("failed to decode gob header: %w", err)
	}
	return m, nil
}

// ToMsg inserts SHAR headers into a nats message
func ToMsg(ctx context.Context, values Values, msg *nats.Msg) error {
	var buf bytes.Buffer
	enc := gob.NewEncoder(&buf)
	err := enc.Encode(values)
	if err != nil {
		return fmt.Errorf("failed to encode gob header: %w", err)
	}
	b64 := base64.StdEncoding.EncodeToString(buf.Bytes())
	msg.Header.Set(natsSharHeader, b64)
	return nil
}
