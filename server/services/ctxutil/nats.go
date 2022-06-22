package ctxutil

import (
	"context"
	"github.com/nats-io/nats.go"
	"go.opentelemetry.io/otel"
)

func LoadNATSHeaderFromContext(ctx context.Context, msg *nats.Msg) {
	if msg.Header == nil {
		msg.Header = make(map[string][]string)
	}
	p := otel.GetTextMapPropagator()
	p.Inject(ctx, natsCarrier{msg})
}

func LoadContextFromNATSHeader(ctx context.Context, msg *nats.Msg) context.Context {
	p := otel.GetTextMapPropagator()
	ctx = p.Extract(ctx, natsCarrier{msg})
	return ctx
}

type natsCarrier struct {
	msg *nats.Msg
}

func (n natsCarrier) Get(key string) string {
	return n.msg.Header.Get(key)
}
func (n natsCarrier) Set(key string, value string) {
	n.msg.Header.Set(key, value)
}
func (n natsCarrier) Keys() []string {
	r := make([]string, 0, len(n.msg.Header))
	for k := range n.msg.Header {
		r = append(r, k)
	}
	return r
}
