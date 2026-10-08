package logging_test

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
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

func TestReplaceAttrLevel(t *testing.T) {
	t.Run("string level does not panic and is preserved as level attribute", func(t *testing.T) {
		var buf bytes.Buffer
		logger := newBufferLogger(&buf)

		logger.Info("test level message", logging.String("level", "high"))

		logMap := parseLogEntry(t, &buf)
		if v, ok := logMap["level"]; !ok || v != "high" {
			t.Errorf("Expected level=high, got: %v", v)
		}
		if sev, ok := logMap["severity"]; !ok || sev != "INFO" {
			t.Errorf("Expected severity=INFO, got: %v", sev)
		}
	})

	t.Run("numeric and nil level does not panic", func(t *testing.T) {
		var buf bytes.Buffer
		logger := newBufferLogger(&buf)

		logger.Info("test int level", logging.Int("level", 42))

		logMap := parseLogEntry(t, &buf)
		if v, ok := logMap["level"]; !ok || v != float64(42) {
			t.Errorf("Expected level=42, got: %v", v)
		}
		if sev, ok := logMap["severity"]; !ok || sev != "INFO" {
			t.Errorf("Expected severity=INFO, got: %v", sev)
		}

		buf.Reset()
		logger.Info("test nil level", slog.Any("level", nil))

		logMapNil := parseLogEntry(t, &buf)
		if v, ok := logMapNil["level"]; !ok || v != nil {
			t.Errorf("Expected level=nil, got: %v (present: %v)", v, ok)
		}
		if sev, ok := logMapNil["severity"]; !ok || sev != "INFO" {
			t.Errorf("Expected severity=INFO, got: %v", sev)
		}
	})
}

func TestReplaceAttrSource(t *testing.T) {
	t.Run("built-in source is renamed to sourceLocation", func(t *testing.T) {
		var buf bytes.Buffer
		logger := newBufferLogger(&buf)

		logger.Info("test source location")

		logMap := parseLogEntry(t, &buf)
		sourceLoc, ok := logMap["logging.googleapis.com/sourceLocation"].(map[string]any)
		if !ok {
			t.Fatalf("Expected logging.googleapis.com/sourceLocation map, got: %v", logMap["logging.googleapis.com/sourceLocation"])
		}
		if _, hasFile := sourceLoc["file"]; !hasFile {
			t.Errorf("Expected file in sourceLocation, got: %v", sourceLoc)
		}
	})

	t.Run("custom source attribute with non-*slog.Source value is not renamed", func(t *testing.T) {
		var buf bytes.Buffer
		logger := newBufferLogger(&buf)

		logger.Info("test message", logging.String("source", "event-source"))

		logMap := parseLogEntry(t, &buf)
		if v, ok := logMap["source"]; !ok || v != "event-source" {
			t.Errorf("Expected source=event-source, got: %v", v)
		}
		if _, ok := logMap["logging.googleapis.com/sourceLocation"]; !ok {
			t.Errorf("Expected logging.googleapis.com/sourceLocation for built-in source")
		}
	})
}

func TestReplaceAttrTime(t *testing.T) {
	t.Run("custom time attribute with non-time.Time value is not renamed", func(t *testing.T) {
		var buf bytes.Buffer
		logger := newBufferLogger(&buf)

		logger.Info("test time message", logging.String("time", "yesterday"))

		logMap := parseLogEntry(t, &buf)
		if _, ok := logMap["timestamp"]; !ok {
			t.Errorf("Expected built-in timestamp to be present")
		}
		if v, ok := logMap["time"]; !ok || v != "yesterday" {
			t.Errorf("Expected time='yesterday', got: %v", v)
		}
	})
}

func TestReplaceAttrWithGroup(t *testing.T) {
	var buf bytes.Buffer
	logger := newBufferLogger(&buf)

	groupedLogger := logger.WithGroup("custom_group")
	groupedLogger.Info(
		"grouped log message",
		logging.String("level", "inner_level"),
		logging.String("time", "inner_time"),
		logging.String("source", "inner_source"),
	)

	logMap := parseLogEntry(t, &buf)

	if v := logMap["message"]; v != "grouped log message" {
		t.Errorf("Expected message='grouped log message', got: %v", v)
	}

	grp, ok := logMap["custom_group"].(map[string]any)
	if !ok {
		t.Fatalf("Expected custom_group map, got: %v", logMap["custom_group"])
	}
	if grp["level"] != "inner_level" {
		t.Errorf("Expected custom_group.level='inner_level', got: %v", grp["level"])
	}
	if grp["time"] != "inner_time" {
		t.Errorf("Expected custom_group.time='inner_time', got: %v", grp["time"])
	}
	if grp["source"] != "inner_source" {
		t.Errorf("Expected custom_group.source='inner_source', got: %v", grp["source"])
	}
}
