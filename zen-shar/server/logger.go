package server

import (
	"fmt"
	"go.uber.org/zap"
)

type NatsLogger struct {
	l *zap.Logger
}

// Log a notice statement
func (n *NatsLogger) Noticef(format string, v ...interface{}) {
	n.l.Info(fmt.Sprintf(format, v...))
}

// Log a warning statement
func (n *NatsLogger) Warnf(format string, v ...interface{}) {
	n.l.Warn(fmt.Sprintf(format, v...))
}

// Log a fatal error
func (n *NatsLogger) Fatalf(format string, v ...interface{}) {
	n.l.Fatal(fmt.Sprintf(format, v...))
}

// Log an error
func (n *NatsLogger) Errorf(format string, v ...interface{}) {
	n.l.Error(fmt.Sprintf(format, v...))
}

// Log a debug statement
func (n *NatsLogger) Debugf(format string, v ...interface{}) {
	n.l.Debug(fmt.Sprintf(format, v...))
}

// Log a trace statement
func (n *NatsLogger) Tracef(format string, v ...interface{}) {
	n.l.Info("trace: " + fmt.Sprintf(format, v...))
}
