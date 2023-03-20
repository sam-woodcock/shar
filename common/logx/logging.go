package logx

import (
	"context"
	"fmt"
	"github.com/go-logr/logr"
	"github.com/nats-io/nats.go"
	"golang.org/x/exp/slog"
	"os"
)

// ContextKey is a custom type to avoid context collision.
type ContextKey string

const (
	CorrelationHeader     = "cid"             // CorrelationHeader is the name of the nats message header for transporting the correlationID.
	CorrelationContextKey = ContextKey("cid") // CorrelationContextKey is the name of the context key used to store the correlationID.
	EcoSystemLoggingKey   = "eco"             // EcoSystemLoggingKey is the name of the logging key used to store the current ecosystem.
	SubsystemLoggingKey   = "sub"             // SubsystemLoggingKey is the name of the logging key used to store the current subsystem.
	CorrelationLoggingKey = "cid"             // CorrelationLoggingKey is the name of the logging key used to store the correlation id.
	AreaLoggingKey        = "loc"             // AreaLoggingKey is the name of the logging key used to store the functional area.
)

// Err will output error message to the log and return the error with additional attributes.
func Err(ctx context.Context, message string, err error, atts ...any) error {
	l, err2 := logr.FromContext(ctx)
	if err2 != nil {
		return fmt.Errorf("error: %w", err)
	}
	if l.Enabled() {
		l.Error(err, message, atts)
	}
	return fmt.Errorf(message+" %s : %w", fmt.Sprint(atts...), err)
}

// SetDefault sets the default logger for an application.  This should be done in tha application's main.go before and call to slog to prevent race conditions.
func SetDefault(level slog.Level, addSource bool, ecosystem string) {
	o := slog.HandlerOptions{
		AddSource:   addSource,
		Level:       level,
		ReplaceAttr: nil,
	}
	h := o.NewTextHandler(os.Stdout)
	slog.SetDefault(slog.New(h).With(slog.String(EcoSystemLoggingKey, ecosystem)))
}

// NatsMessageLoggingEntrypoint returns a new logger and a context containing the logger for use when a new NATS message arrives.
func NatsMessageLoggingEntrypoint(ctx context.Context, subsystem string, hdr nats.Header) (context.Context, *slog.Logger) {
	cid := hdr.Get(CorrelationHeader)
	return loggingEntrypoint(ctx, subsystem, cid)
}

type contextLoggerKey string

var ctxLogKey contextLoggerKey = "__log"

// ContextWith obtains a new logger with an area parameter.  Typically it should be used when obtaining a logger within a programmatic boundary.
func ContextWith(ctx context.Context, area string) (context.Context, *slog.Logger) {
	logger := FromContext(ctx).With(AreaLoggingKey, area)
	return NewContext(ctx, logger), logger
}

func NewContext(ctx context.Context, logger *slog.Logger) context.Context {
	return context.WithValue(ctx, ctxLogKey, logger)
}

// ContextWith obtains a new logger with an area parameter.  Typically it should be used when obtaining a logger within a programmatic boundary.
func FromContext(ctx context.Context) *slog.Logger {
	var cl *slog.Logger
	l := ctx.Value(ctxLogKey)
	if l == nil {
		cl = slog.Default()
	} else {
		cl = l.(*slog.Logger)
	}
	return cl
}

func loggingEntrypoint(ctx context.Context, subsystem string, correlationId string) (context.Context, *slog.Logger) {
	logger := FromContext(ctx).With(slog.String(SubsystemLoggingKey, subsystem), slog.String(CorrelationLoggingKey, correlationId))
	ctx = NewContext(ctx, logger)
	ctx = context.WithValue(ctx, CorrelationContextKey, correlationId)
	return ctx, logger
}
