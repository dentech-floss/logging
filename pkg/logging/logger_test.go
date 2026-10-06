package logging_test

import (
	"bytes"
	"context"
	"encoding/json"
	"testing"

	"github.com/dentech-floss/logging/pkg/logging"
	"go.opentelemetry.io/otel/trace"
)

func TestLogger(t *testing.T) {
	var buf bytes.Buffer

	logger := logging.NewLogger(&logging.LoggerConfig{
		ProjectID:   "test-project",
		ServiceName: "test-service",
		MinLevel:    logging.DebugLevel,

		Output: &buf,
	})

	ctx := trace.ContextWithSpanContext(context.Background(), trace.NewSpanContext(
		trace.SpanContextConfig{
			TraceID: [16]byte{0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07, 0x08, 0x09, 0x0a, 0x0b, 0x0c, 0x0d, 0x0e, 0x0f, 0x10},
			SpanID:  [8]byte{0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07, 0x08},
		}))

	logger.InfoContext(ctx, "This is a test log message", logging.String("key", "value"))

	var logMap map[string]any
	if err := json.Unmarshal(buf.Bytes(), &logMap); err != nil {
		t.Fatalf("Failed to parse JSON log: %v", err)
	}

	if traceID, ok := logMap["logging.googleapis.com/trace"]; !ok ||
		traceID != "projects/test-project/traces/0102030405060708090a0b0c0d0e0f10" {
		t.Errorf("Expected trace ID not found or incorrect, got: %v", traceID)
	}
}

func TestNoticeLevel(t *testing.T) {
	var buf bytes.Buffer

	logger := logging.NewLogger(&logging.LoggerConfig{
		ProjectID:   "test-project",
		ServiceName: "test-service",
		MinLevel:    logging.DebugLevel,

		Output: &buf,
	})

	logger.Notice("This is a notice message", logging.String("key", "value"))

	var logMap map[string]any
	if err := json.Unmarshal(buf.Bytes(), &logMap); err != nil {
		t.Fatalf("Failed to parse JSON log: %v", err)
	}

	if severity, ok := logMap["severity"]; !ok || severity != "NOTICE" {
		t.Errorf("Expected severity NOTICE, got: %v", severity)
	}

	if msg, ok := logMap["message"]; !ok || msg != "This is a notice message" {
		t.Errorf("Expected notice message, got: %v", msg)
	}

	// Notice is below Warn, so it must not include a stacktrace.
	if _, ok := logMap["stacktrace"]; ok {
		t.Errorf("Did not expect a stacktrace for NOTICE level")
	}
}

func newBufferLogger(buf *bytes.Buffer) *logging.Logger {
	return logging.NewLogger(&logging.LoggerConfig{
		ProjectID:   "test-project",
		ServiceName: "test-service",
		MinLevel:    logging.DebugLevel,

		Output: buf,
	})
}

func parseLogEntry(t *testing.T, buf *bytes.Buffer) map[string]any {
	t.Helper()

	var logMap map[string]any
	if err := json.Unmarshal(buf.Bytes(), &logMap); err != nil {
		t.Fatalf("Failed to parse JSON log: %v", err)
	}
	return logMap
}

func TestLoggerFromContextNeverReturnsNil(t *testing.T) {
	tests := []struct {
		name string
		ctx  context.Context
	}{
		{
			name: "no logger in context",
			ctx:  context.Background(),
		},
		{
			name: "nil logger in context",
			ctx:  logging.ContextWithLogger(context.Background(), nil),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			logger := logging.LoggerFromContext(tt.ctx)
			if logger == nil {
				t.Fatal("Expected a logger, got nil")
			}
			// Must not panic
			logger.InfoContext(tt.ctx, "This is a test log message")
		})
	}
}

func TestLoggerFromContextReturnsStoredLogger(t *testing.T) {
	var buf bytes.Buffer
	logger := newBufferLogger(&buf)

	ctx := logging.ContextWithLogger(context.Background(), logger)

	if got := logging.LoggerFromContext(ctx); got != logger {
		t.Errorf("Expected the stored logger, got: %v", got)
	}
}

func TestContextWithFields(t *testing.T) {
	var buf bytes.Buffer
	logger := newBufferLogger(&buf)

	ctx := logging.ContextWithFields(context.Background(), logging.String("first", "1"))
	ctx = logging.ContextWithFields(ctx, logging.String("second", "2"), logging.Int("third", 3))

	logger.InfoContext(ctx, "This is a test log message")

	logMap := parseLogEntry(t, &buf)
	if v := logMap["first"]; v != "1" {
		t.Errorf("Expected field first=1, got: %v", v)
	}
	if v := logMap["second"]; v != "2" {
		t.Errorf("Expected field second=2, got: %v", v)
	}
	if v := logMap["third"]; v != float64(3) {
		t.Errorf("Expected field third=3, got: %v", v)
	}
}

func TestContextWithFieldsDoesNotLeakBetweenSiblings(t *testing.T) {
	var buf bytes.Buffer
	logger := newBufferLogger(&buf)

	parent := logging.ContextWithFields(context.Background(), logging.String("parent", "p"))
	_ = logging.ContextWithFields(parent, logging.String("sibling", "a"))
	ctx := logging.ContextWithFields(parent, logging.String("other", "b"))

	logger.InfoContext(ctx, "This is a test log message")

	logMap := parseLogEntry(t, &buf)
	if v := logMap["parent"]; v != "p" {
		t.Errorf("Expected field parent=p, got: %v", v)
	}
	if v := logMap["other"]; v != "b" {
		t.Errorf("Expected field other=b, got: %v", v)
	}
	if v, ok := logMap["sibling"]; ok {
		t.Errorf("Did not expect the sibling field, got: %v", v)
	}
}
