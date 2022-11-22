package logx

import (
	"context"
	"fmt"
	"golang.org/x/exp/slog"
	"os"
)

// Err will output error message to the log and return the error with additional attributes
func Err(ctx context.Context, message string, err error, atts ...any) error {
	l := slog.FromContext(ctx)
	if !l.Enabled(slog.ErrorLevel) {
		return err
	}
	l.Error(message, err, atts)
	return fmt.Errorf(message+" %s : %w", fmt.Sprint(atts...), err)
}

// SetDefault sets the default logger for an application.  This should be done in tha application's main.go before and call to slog to prevent race conditions.
func SetDefault(level slog.Level, addSource bool, ecosystem string) {
	o := slog.HandlerOptions{
		AddSource:   addSource,
		Level:       slog.NewAtomicLevel(level),
		ReplaceAttr: nil,
	}
	h := o.NewTextHandler(os.Stdout)
	slog.SetDefault(slog.New(h).With(slog.String("eco", ecosystem)))
}

// LoggingEntrypoint returns a new logger and a context containing the logger for use during entry points.  An entry point is any code location where correlation has been obtained.
func LoggingEntrypoint(ctx context.Context, subsystem string, correlationId string) (context.Context, *slog.Logger) {
	logger := slog.Default().With(slog.String("_s", subsystem), slog.String("_cid", correlationId))
	return slog.NewContext(ctx, logger), logger
}

// ContextWith obtains a new logger with an area parameter.  Typically it should be used when obtaining a logger within a programmatic boundary.
func ContextWith(ctx context.Context, area string) (context.Context, *slog.Logger) {
	logger := slog.FromContext(ctx).With("_a", area)
	return slog.NewContext(ctx, logger), logger
}
