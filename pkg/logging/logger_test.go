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
