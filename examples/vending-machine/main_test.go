package main

import (
	"context"
	_ "embed"
	"log/slog"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

//go:embed testdata/expected.log.txt
var expectedLogTxt string

// Verification policy (definition of done and falsification condition: state/machine_test.go):
//
//	Guaranteed: the log lines main's scenario emits match testdata/expected.log.txt.
//	Not guaranteed: the log format itself (slog's handler), the 10 s timeout in real time
//	  (driven by FastForward), failure paths absent from the golden, the stop Transition
//	  (the scenario does not stop the machine).
//	Not automated: none.
//	Strength check required: "TestVendingMachine" (any changed line of the blocked scenario turns it red)

func TestVendingMachine(t *testing.T) {
	// Setup: capture logs
	capture := &logCapture{}
	testLogger := slog.New(capture)

	// Replace global log temporarily
	oldLog := log
	log = testLogger
	t.Cleanup(func() {
		log = oldLog
	})

	// Execute
	main()
	assert.Equal(t, expectedLogTxt, strings.Join(capture.messages, "\n"))
}

// logCapture is a custom slog.Handler that captures log messages.
type logCapture struct {
	messages []string
}

func (c *logCapture) Enabled(context.Context, slog.Level) bool {
	return true
}

func (c *logCapture) Handle(_ context.Context, r slog.Record) error {
	var sb strings.Builder
	sb.WriteString(r.Message)
	r.Attrs(func(a slog.Attr) bool {
		sb.WriteString(" ")
		sb.WriteString(a.Key)
		sb.WriteString("=")
		sb.WriteString(formatValue(a.Value))
		return true
	})
	c.messages = append(c.messages, sb.String())
	return nil
}

func (c *logCapture) WithAttrs(attrs []slog.Attr) slog.Handler {
	return c
}

func (c *logCapture) WithGroup(name string) slog.Handler {
	return c
}

func formatValue(v slog.Value) string {
	s := v.String()
	if needsQuoting(s) {
		return `"` + s + `"`
	}
	return s
}

func needsQuoting(s string) bool {
	return strings.ContainsAny(s, " \t\n\"")
}
