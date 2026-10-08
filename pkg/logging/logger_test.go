package logging_test

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"testing"
	"time"

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
		if _, hasLine := sourceLoc["line"]; !hasLine {
			t.Errorf("Expected line in sourceLocation, got: %v", sourceLoc)
		}
		if _, hasFunc := sourceLoc["function"]; !hasFunc {
			t.Errorf("Expected function in sourceLocation, got: %v", sourceLoc)
		}
		if _, hasSource := logMap["source"]; hasSource {
			t.Errorf("Did not expect raw source key when AddSource is true")
		}
	})

	t.Run("custom source attribute with non-*slog.Source value is not renamed", func(t *testing.T) {
		testCases := []struct {
			name     string
			attr     slog.Attr
			expected any
		}{
			{
				name:     "string value",
				attr:     slog.String("source", "event-source"),
				expected: "event-source",
			},
			{
				name:     "int value",
				attr:     slog.Int("source", 123),
				expected: float64(123),
			},
			{
				name:     "bool value",
				attr:     slog.Bool("source", true),
				expected: true,
			},
			{
				name:     "nil value",
				attr:     slog.Any("source", nil),
				expected: nil,
			},
		}

		for _, tc := range testCases {
			t.Run(tc.name, func(t *testing.T) {
				var buf bytes.Buffer
				logger := newBufferLogger(&buf)

				logger.Info("test message", tc.attr)

				logMap := parseLogEntry(t, &buf)
				if v, ok := logMap["source"]; !ok || v != tc.expected {
					t.Errorf("Expected source=%v, got: %v (found: %v)", tc.expected, v, ok)
				}
				if _, ok := logMap["logging.googleapis.com/sourceLocation"]; !ok {
					t.Errorf("Expected logging.googleapis.com/sourceLocation for built-in source")
				}
			})
		}
	})

	t.Run("explicit *slog.Source attribute with source key is preserved without colliding with sourceLocation", func(t *testing.T) {
		var buf bytes.Buffer
		logger := newBufferLogger(&buf)

		customSource := &slog.Source{
			File:     "custom_file.go",
			Line:     42,
			Function: "customFunction",
		}
		logger.Info("test message", slog.Any("source", customSource))

		logMap := parseLogEntry(t, &buf)
		// User source should remain under "source"
		userSource, ok := logMap["source"].(map[string]any)
		if !ok {
			t.Fatalf("Expected source attribute map, got: %v", logMap["source"])
		}
		if userSource["file"] != "custom_file.go" {
			t.Errorf("Expected user source file custom_file.go, got: %v", userSource["file"])
		}
		if userSource["line"] != float64(42) {
			t.Errorf("Expected user source line 42, got: %v", userSource["line"])
		}
		if userSource["function"] != "customFunction" {
			t.Errorf("Expected user source function customFunction, got: %v", userSource["function"])
		}

		// Built-in sourceLocation should still be present
		sourceLoc, ok := logMap["logging.googleapis.com/sourceLocation"].(map[string]any)
		if !ok {
			t.Fatalf("Expected logging.googleapis.com/sourceLocation map, got: %v", logMap["logging.googleapis.com/sourceLocation"])
		}
		if _, hasFile := sourceLoc["file"]; !hasFile {
			t.Errorf("Expected file in sourceLocation, got: %v", sourceLoc)
		}
	})
}

func TestReplaceAttrLevel(t *testing.T) {
	t.Run("string level does not panic and is preserved as level attribute", func(t *testing.T) {
		var buf bytes.Buffer
		logger := newBufferLogger(&buf)

		// Must not panic
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

	t.Run("slog.Level value is preserved as level attribute without colliding with severity", func(t *testing.T) {
		var buf bytes.Buffer
		logger := newBufferLogger(&buf)

		logger.Info("test slog.Level value", slog.Any("level", slog.LevelWarn))

		logMap := parseLogEntry(t, &buf)
		if _, ok := logMap["level"]; !ok {
			t.Errorf("Expected level attribute to be present")
		}
		if sev, ok := logMap["severity"]; !ok || sev != "INFO" {
			t.Errorf("Expected severity=INFO, got: %v", sev)
		}
	})
}

func TestReplaceAttrMsg(t *testing.T) {
	t.Run("custom msg attribute does not collide with message", func(t *testing.T) {
		var buf bytes.Buffer
		logger := newBufferLogger(&buf)

		logger.Info("primary message", logging.String("msg", "user_msg_value"))

		logMap := parseLogEntry(t, &buf)
		if msg, ok := logMap["message"]; !ok || msg != "primary message" {
			t.Errorf("Expected message='primary message', got: %v", msg)
		}
		if customMsg, ok := logMap["msg"]; !ok || customMsg != "user_msg_value" {
			t.Errorf("Expected msg='user_msg_value', got: %v", customMsg)
		}
	})

	t.Run("custom msg in ContextWithFields does not collide with message", func(t *testing.T) {
		var buf bytes.Buffer
		logger := newBufferLogger(&buf)

		ctx := logging.ContextWithFields(context.Background(), logging.String("msg", "ctx_msg_value"))
		logger.InfoContext(ctx, "primary message")

		logMap := parseLogEntry(t, &buf)
		if msg, ok := logMap["message"]; !ok || msg != "primary message" {
			t.Errorf("Expected message='primary message', got: %v", msg)
		}
		if customMsg, ok := logMap["msg"]; !ok || customMsg != "ctx_msg_value" {
			t.Errorf("Expected msg='ctx_msg_value', got: %v", customMsg)
		}
	})

	t.Run("custom msg in Logger.With does not collide with message", func(t *testing.T) {
		var buf bytes.Buffer
		logger := newBufferLogger(&buf)

		withLogger := logger.With(logging.String("msg", "with_msg_value"))
		withLogger.Info("primary message")

		logMap := parseLogEntry(t, &buf)
		if msg, ok := logMap["message"]; !ok || msg != "primary message" {
			t.Errorf("Expected message='primary message', got: %v", msg)
		}
		if customMsg, ok := logMap["msg"]; !ok || customMsg != "with_msg_value" {
			t.Errorf("Expected msg='with_msg_value', got: %v", customMsg)
		}
	})
}

func TestReplaceAttrTime(t *testing.T) {
	t.Run("custom time attribute does not collide with timestamp", func(t *testing.T) {
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

	t.Run("slog.Time value is preserved as time attribute without colliding with timestamp", func(t *testing.T) {
		var buf bytes.Buffer
		logger := newBufferLogger(&buf)

		logger.Info("test slog.Time value", slog.Time("time", parseTimeOrPanic("2026-01-01T00:00:00Z")))

		logMap := parseLogEntry(t, &buf)
		if _, ok := logMap["timestamp"]; !ok {
			t.Errorf("Expected built-in timestamp to be present")
		}
		if v, ok := logMap["time"]; !ok || v != "2026-01-01T00:00:00Z" {
			t.Errorf("Expected time='2026-01-01T00:00:00Z', got: %v", v)
		}
	})
}

type sampleGroupValuer struct{}

func (sampleGroupValuer) LogValue() slog.Value {
	return slog.GroupValue(slog.String("inner", "value"))
}

func parseTimeOrPanic(s string) time.Time {
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		panic(err)
	}
	return t
}

func TestReplaceAttrGroupsWithReservedKeys(t *testing.T) {
	t.Run("group attribute with reserved keys has no NUL prefix in output", func(t *testing.T) {
		var buf bytes.Buffer
		logger := newBufferLogger(&buf)

		logger.Info("test group message",
			slog.Group("msg", slog.String("a", "b")),
			slog.Group("time", slog.Int("x", 1)),
			slog.Group("level", slog.String("l", "val")),
			slog.Group("source", slog.String("s", "val")),
		)

		logMap := parseLogEntry(t, &buf)

		// Must not contain any \u0000 prefixes
		for k := range logMap {
			if len(k) > 0 && k[0] == '\x00' {
				t.Fatalf("Found unexpected NUL prefix in key: %q", k)
			}
		}

		// Built-in fields mapped properly
		if v := logMap["message"]; v != "test group message" {
			t.Errorf("Expected message='test group message', got: %v", v)
		}

		// Group attributes preserved with exact original keys
		if msgGrp, ok := logMap["msg"].(map[string]any); !ok || msgGrp["a"] != "b" {
			t.Errorf("Expected group msg={a: b}, got: %v", logMap["msg"])
		}
		if timeGrp, ok := logMap["time"].(map[string]any); !ok || timeGrp["x"] != float64(1) {
			t.Errorf("Expected group time={x: 1}, got: %v", logMap["time"])
		}
		if levelGrp, ok := logMap["level"].(map[string]any); !ok || levelGrp["l"] != "val" {
			t.Errorf("Expected group level={l: val}, got: %v", logMap["level"])
		}
		if srcGrp, ok := logMap["source"].(map[string]any); !ok || srcGrp["s"] != "val" {
			t.Errorf("Expected group source={s: val}, got: %v", logMap["source"])
		}
	})

	t.Run("LogValuer resolving to GroupValue has no NUL prefix", func(t *testing.T) {
		var buf bytes.Buffer
		logger := newBufferLogger(&buf)

		logger.Info("test log valuer",
			slog.Any("level", sampleGroupValuer{}),
			slog.Any("msg", sampleGroupValuer{}),
		)

		logMap := parseLogEntry(t, &buf)

		for k := range logMap {
			if len(k) > 0 && k[0] == '\x00' {
				t.Fatalf("Found unexpected NUL prefix in key: %q", k)
			}
		}

		if lvlGrp, ok := logMap["level"].(map[string]any); !ok || lvlGrp["inner"] != "value" {
			t.Errorf("Expected group level={inner: value}, got: %v", logMap["level"])
		}
		if msgGrp, ok := logMap["msg"].(map[string]any); !ok || msgGrp["inner"] != "value" {
			t.Errorf("Expected group msg={inner: value}, got: %v", logMap["msg"])
		}
	})

	t.Run("group in With and ContextWithFields has no NUL prefix", func(t *testing.T) {
		var buf bytes.Buffer
		logger := newBufferLogger(&buf)

		withLogger := logger.With(slog.Group("time", slog.String("with_k", "with_v")))
		ctx := logging.ContextWithFields(context.Background(), slog.Group("level", slog.String("ctx_k", "ctx_v")))

		withLogger.InfoContext(ctx, "test combined groups")

		logMap := parseLogEntry(t, &buf)

		for k := range logMap {
			if len(k) > 0 && k[0] == '\x00' {
				t.Fatalf("Found unexpected NUL prefix in key: %q", k)
			}
		}

		if timeGrp, ok := logMap["time"].(map[string]any); !ok || timeGrp["with_k"] != "with_v" {
			t.Errorf("Expected group time={with_k: with_v}, got: %v", logMap["time"])
		}
		if lvlGrp, ok := logMap["level"].(map[string]any); !ok || lvlGrp["ctx_k"] != "ctx_v" {
			t.Errorf("Expected group level={ctx_k: ctx_v}, got: %v", logMap["level"])
		}
	})

	t.Run("inlined group with empty key does not cause key collision", func(t *testing.T) {
		var buf bytes.Buffer
		logger := newBufferLogger(&buf)

		logger.Info(
			"1st message",
			slog.Group("",
				slog.String("msg", "inl_msg"),
				slog.String("time", "12:00"),
				slog.String("level", "high"),
				slog.String("source", "inl_source"),
			),
		)

		logMap := parseLogEntry(t, &buf)

		if v := logMap["message"]; v != "1st message" {
			t.Errorf("Expected message='1st message', got: %v", v)
		}
		if v := logMap["msg"]; v != "inl_msg" {
			t.Errorf("Expected msg='inl_msg', got: %v", v)
		}
		if v := logMap["time"]; v != "12:00" {
			t.Errorf("Expected time='12:00', got: %v", v)
		}
		if v := logMap["level"]; v != "high" {
			t.Errorf("Expected level='high', got: %v", v)
		}
		if v := logMap["source"]; v != "inl_source" {
			t.Errorf("Expected source='inl_source', got: %v", v)
		}
	})
}

type countingValuer struct {
	count *int
}

func (c countingValuer) LogValue() slog.Value {
	*c.count++
	return slog.StringValue("hello")
}

func TestReplaceAttrLogValuerCallCount(t *testing.T) {
	var buf bytes.Buffer
	logger := newBufferLogger(&buf)

	callCount := 0
	logger.Info("test call count", slog.Any("msg", countingValuer{count: &callCount}))

	logMap := parseLogEntry(t, &buf)
	if v := logMap["msg"]; v != "hello" {
		t.Errorf("Expected msg='hello', got: %v", v)
	}

	// Should only be resolved once in escapeAttr and once by slog JSONHandler (total <= 2)
	if callCount > 2 {
		t.Errorf("Expected LogValue() to be called at most 2 times, called: %d", callCount)
	}
}

func TestReplaceAttrWithGroupPreservesKeys(t *testing.T) {
	var buf bytes.Buffer
	logger := newBufferLogger(&buf)

	groupedLogger := logger.WithGroup("user_ctx")
	groupedLogger.Info(
		"main grouped message",
		slog.String("msg", "inner_msg"),
		slog.String("level", "inner_level"),
		slog.String("time", "inner_time"),
		slog.String("source", "inner_source"),
	)

	logMap := parseLogEntry(t, &buf)

	// Built-in message at top level
	if v := logMap["message"]; v != "main grouped message" {
		t.Errorf("Expected top-level message='main grouped message', got: %v", v)
	}

	// Inside the group, keys must NOT be renamed to message, severity, timestamp, sourceLocation
	grp, ok := logMap["user_ctx"].(map[string]any)
	if !ok {
		t.Fatalf("Expected group user_ctx map, got: %v", logMap["user_ctx"])
	}
	if grp["msg"] != "inner_msg" {
		t.Errorf("Expected user_ctx.msg='inner_msg', got: %v", grp["msg"])
	}
	if grp["level"] != "inner_level" {
		t.Errorf("Expected user_ctx.level='inner_level', got: %v", grp["level"])
	}
	if grp["time"] != "inner_time" {
		t.Errorf("Expected user_ctx.time='inner_time', got: %v", grp["time"])
	}
	if grp["source"] != "inner_source" {
		t.Errorf("Expected user_ctx.source='inner_source', got: %v", grp["source"])
	}
}

func TestReplaceAttrReservedKeysCombined(t *testing.T) {
	var buf bytes.Buffer
	logger := newBufferLogger(&buf)

	logger.Info(
		"main message",
		logging.String("msg", "child_msg"),
		logging.String("time", "12:30"),
		logging.String("level", "high"),
		logging.String("source", "events_topic"),
	)

	logMap := parseLogEntry(t, &buf)

	// Built-in fields mapped to Cloud Logging format
	if v := logMap["message"]; v != "main message" {
		t.Errorf("Expected message='main message', got: %v", v)
	}
	if v := logMap["severity"]; v != "INFO" {
		t.Errorf("Expected severity='INFO', got: %v", v)
	}
	if _, ok := logMap["timestamp"]; !ok {
		t.Errorf("Expected timestamp to be present")
	}
	if _, ok := logMap["logging.googleapis.com/sourceLocation"]; !ok {
		t.Errorf("Expected logging.googleapis.com/sourceLocation to be present")
	}

	// User fields preserved under original names
	if v := logMap["msg"]; v != "child_msg" {
		t.Errorf("Expected msg='child_msg', got: %v", v)
	}
	if v := logMap["time"]; v != "12:30" {
		t.Errorf("Expected time='12:30', got: %v", v)
	}
	if v := logMap["level"]; v != "high" {
		t.Errorf("Expected level='high', got: %v", v)
	}
	if v := logMap["source"]; v != "events_topic" {
		t.Errorf("Expected source='events_topic', got: %v", v)
	}
}
