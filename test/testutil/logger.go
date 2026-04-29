package testutil

import (
	"fmt"
	"strings"
	"testing"
	"time"
)

// Logger is a tiny test-focused logger that writes through testing.TB.
// It keeps logs visible in `go test -v` output and bound to test lifecycle.
type Logger struct {
	t      testing.TB
	prefix string
}

func NewLogger(t testing.TB, prefix string) *Logger {
	t.Helper()
	return &Logger{
		t:      t,
		prefix: strings.TrimSpace(prefix),
	}
}

func (l *Logger) Infof(format string, args ...any) {
	l.logf("INFO", format, args...)
}

func (l *Logger) Warnf(format string, args ...any) {
	l.logf("WARN", format, args...)
}

func (l *Logger) Errorf(format string, args ...any) {
	l.logf("ERROR", format, args...)
}

func (l *Logger) logf(level string, format string, args ...any) {
	l.t.Helper()
	msg := fmt.Sprintf(format, args...)
	now := time.Now().UTC().Format(time.RFC3339Nano)
	if l.prefix != "" {
		l.t.Logf("%s [%s] [%s] %s", now, level, l.prefix, msg)
		return
	}
	l.t.Logf("%s [%s] %s", now, level, msg)
}
