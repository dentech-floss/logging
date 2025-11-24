// Package logging provides a thin abstraction layer around slog.
//
// Why wrap slog fields?
//
// We intentionally provide helpers like String, Int, etc. even
// though they currently delegate directly to slog.String, slog.Int, etc.
// This is not accidental "extra code," but a deliberate design choice:
//
//   - Consistency: all log fields in our codebase are constructed via
//     logging.Xxx(), which makes call sites uniform and easy to scan.
//
//   - Future-proofing seam: should we need to enforce redaction of sensitive
//     values, normalize units (durations in ms, sizes in bytes), or adopt a
//     different logging backend, we can do so centrally without touching
//     thousands of call sites.
//
//   - Performance: we can encourage use of typed helpers over AnyField to
//     avoid reflection overhead in hot paths.
//
// Go idiom tends to avoid unnecessary abstraction; we believe the small cost
// of these thin wrappers is outweighed by the consistency, flexibility, and
// explicit seam they provide. For now most helpers are thin, but the design
// allows us to layer in policy when needed (e.g. redaction, normalization).
//
// Developers new to this codebase should not assume Xxx() does anything
// magical today. Think of it as a hedge that keeps our logging consistent and
// adaptable.
package logging

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"os"
	"runtime/debug"
	"strings"
	"time"

	"go.opentelemetry.io/otel/trace"

	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
)

// A Level is a logging priority. Higher levels are more important.
type Level int8

const (
	// DebugLevel logs are typically voluminous, and are usually disabled in
	// production.
	DebugLevel Level = -4
	// InfoLevel is the default logging priority.
	InfoLevel = 0
	// WarnLevel logs are more important than Info, but don't need individual
	// human review.
	WarnLevel = 4
	// ErrorLevel logs are high-priority. If an application is running smoothly,
	// it shouldn't generate any error-level logs.
	ErrorLevel = 8
	// DPanicLevel logs are particularly important errors. In development the
	// logger panics after writing the message.
	DPanicLevel = 9
	// PanicLevel logs a message, then panics.
	PanicLevel = 16
	// FatalLevel logs a message, then calls os.Exit(1).
	FatalLevel = 32
)

type Logger struct {
	*slog.Logger
}

type LoggerWithContext struct {
	ctx context.Context
	l   *Logger
}

type LoggerConfig struct {
	ProjectID   string
	ServiceName string
	MinLevel    Level

	Output io.Writer
}

type (
	loggerFieldsContextKey struct{}
	loggerContextKey       struct{}
)

type spanContextLogHandler struct {
	slog.Handler
	ProjectID string
}

func NewLogger(config *LoggerConfig) *Logger {
	output := config.Output
	if output == nil {
		output = os.Stdout
	}

	jsonHandler := slog.NewJSONHandler(output, &slog.HandlerOptions{
		AddSource:   true,
		ReplaceAttr: replacer,
		Level:       slog.Level(config.MinLevel),
	})
	instrumentedHandler := handlerWithSpanContext(
		config.ProjectID,
		jsonHandler,
	)
	log := slog.New(instrumentedHandler)

	return &Logger{
		Logger: log.With(slog.Group("serviceContext", String(
			"service",
			config.ServiceName,
		))),
	}
}

func ContextWithLogger(ctx context.Context, logger *Logger) context.Context {
	return context.WithValue(ctx, loggerContextKey{}, logger)
}

func LoggerFromContext(ctx context.Context) *Logger {
	logger, ok := ctx.Value(loggerContextKey{}).(*Logger)
	if !ok {
		return nil
	}

	return logger
}

// Deprecated: for backwards compatibility. Use ContextWithLogger instead.
func (l *Logger) WithContext(
	ctx context.Context,
	args ...any,
) *LoggerWithContext {
	log := &LoggerWithContext{
		ctx: ctx,
		l:   l.With(args...),
	}

	return log
}

// Deprecated: for backwards compatibility.
func (l *Logger) Sync() error {
	return nil
}

func (l *Logger) With(args ...any) *Logger {
	log := l.Logger.With(args...)

	return &Logger{Logger: log}
}

// PanicContext logs at [PanicLevel] with the given context and then panics with the given message.
//
// Parameters:
//
//	ctx - the context for logging
//	msg - the message to log and panic with
//	args - additional arguments for formatting the log message
func (l *Logger) PanicContext(
	ctx context.Context,
	msg string,
	args ...any,
) {
	l.Log(ctx, slog.Level(PanicLevel), msg, args...)
	panic(msg)
}

// Panic logs at [PanicLevel] and then panics with the given message.
//
// Parameters:
//
//	msg - the message to log and panic with
//	args - additional arguments for formatting the log message
func (l *Logger) Panic(msg string, args ...any) {
	l.PanicContext(context.Background(), msg, args...)
	panic(msg)
}

// FatalContext logs at [FatalLevel] using the provided context, then terminates the application.
//
// Parameters:
//
//	ctx - the context for logging
//	msg - the message to log before exiting
//	args - additional arguments for formatting the log message
func (l *Logger) FatalContext(
	ctx context.Context,
	msg string,
	args ...any,
) {
	l.Log(ctx, slog.Level(FatalLevel), msg, args...)
	os.Exit(1)
}

// Fatal logs at [FatalLevel] and then terminates the application.
//
// Parameters:
//
//	msg - the message to log before exiting
//	args - additional arguments for formatting the log message
func (l *Logger) Fatal(msg string, args ...any) {
	l.FatalContext(context.Background(), msg, args...)
	os.Exit(1)
}

func (lc *LoggerWithContext) With(args ...any) *LoggerWithContext {
	return &LoggerWithContext{
		ctx: lc.ctx,
		l:   lc.l.With(args...),
	}
}

// Deprecated: for backwards compatibility. Use ContextWithLogger instead.
func (lc *LoggerWithContext) Context() context.Context {
	return lc.ctx
}

func (lc *LoggerWithContext) Debug(
	msg string,
	args ...any,
) {
	lc.l.DebugContext(lc.ctx, msg, args...)
}

func (lc *LoggerWithContext) Info(
	msg string,
	args ...any,
) {
	lc.l.InfoContext(lc.ctx, msg, args...)
}

func (lc *LoggerWithContext) Warn(
	msg string,
	args ...any,
) {
	lc.l.WarnContext(lc.ctx, msg, args...)
}

func (lc *LoggerWithContext) Error(
	msg string,
	args ...any,
) {
	lc.l.ErrorContext(lc.ctx, msg, args...)
}

func (lc *LoggerWithContext) Panic(
	msg string,
	args ...any,
) {
	lc.l.PanicContext(lc.ctx, msg, args...)
}

func (lc *LoggerWithContext) Fatal(
	msg string,
	args ...any,
) {
	lc.l.FatalContext(lc.ctx, msg, args...)
}

func handlerWithSpanContext(projectID string, handler slog.Handler) *spanContextLogHandler {
	return &spanContextLogHandler{
		Handler:   handler,
		ProjectID: projectID,
	}
}

func (t *spanContextLogHandler) Enabled(ctx context.Context, level slog.Level) bool {
	return t.Handler.Enabled(ctx, level)
}

// Handle overrides slog.Handler's Handle method. This adds attributes from the
// span context to the slog.Record.
func (t *spanContextLogHandler) Handle(ctx context.Context, record slog.Record) error {
	attrs := LoggerFieldsFromContext(ctx)
	if len(attrs) != 0 {
		record.AddAttrs(attrs...)
	}

	if record.Level >= slog.LevelWarn {
		stack := debug.Stack()
		record.AddAttrs(
			slog.String("stacktrace",
				trimStack(stack),
			),
		)
	}

	if s := trace.SpanContextFromContext(ctx); s.IsValid() {
		record.AddAttrs(
			slog.Any(
				"logging.googleapis.com/trace",
				fmt.Sprintf("projects/%s/traces/%s",
					t.ProjectID,
					s.TraceID(),
				),
			),
			slog.Any("logging.googleapis.com/spanId", s.SpanID()),
			slog.Bool("logging.googleapis.com/trace_sampled", s.TraceFlags().IsSampled()),
		)
	}
	return t.Handler.Handle(ctx, record)
}

func (t *spanContextLogHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return &spanContextLogHandler{
		ProjectID: t.ProjectID,
		Handler:   t.Handler.WithAttrs(attrs),
	}
}

func (t *spanContextLogHandler) WithGroup(name string) slog.Handler {
	return &spanContextLogHandler{
		ProjectID: t.ProjectID,
		Handler:   t.Handler.WithGroup(name),
	}
}

func ContextWithLoggerFields(
	ctx context.Context,
	attrs []slog.Attr,
) context.Context {
	return context.WithValue(
		ctx,
		loggerFieldsContextKey{},
		attrs,
	)
}

func LoggerFieldsFromContext(
	ctx context.Context,
) []slog.Attr {
	var loggerFields []slog.Attr
	if v := ctx.Value(loggerFieldsContextKey{}); v != nil {
		loggerFields = append(loggerFields, v.([]slog.Attr)...)
	}
	return loggerFields
}

func replacer(groups []string, a slog.Attr) slog.Attr {
	// Rename attribute keys to match Cloud Logging structured log format
	switch a.Key {
	case slog.LevelKey:
		a.Key = "severity"
		level := a.Value.Any().(slog.Level)
		// Map slog.Level string values to Cloud Logging LogSeverity
		// https://cloud.google.com/logging/docs/reference/v2/rest/v2/LogEntry#LogSeverity
		switch level {
		case slog.LevelDebug:
			a.Value = slog.StringValue("DEBUG")
		case slog.LevelInfo:
			a.Value = slog.StringValue("INFO")
		case slog.LevelWarn:
			a.Value = slog.StringValue("WARNING")
		case slog.LevelError, DPanicLevel:
			a.Value = slog.StringValue("ERROR")
		case PanicLevel:
			a.Value = slog.StringValue("CRITICAL")
		case FatalLevel:
			a.Value = slog.StringValue("EMERGENCY")
		}
	case slog.TimeKey:
		a.Key = "timestamp"
	case slog.MessageKey:
		a.Key = "message"
	case slog.SourceKey:
		a.Key = "logging.googleapis.com/sourceLocation"
	}
	return a
}

var stackSkipPrefixes = []string{
	"runtime/debug.",
	"log/slog.",
	"github.com/dentech-floss/logging/pkg/logging.",
}

func trimStack(stack []byte) string {
	s := string(stack)
	lines := strings.Split(s, "\n")
	if len(lines) <= 1 {
		return s
	}

	startLine := 1

	for i := 1; i < len(lines)-1; i += 2 {
		packageLine := lines[i]
		isNoisy := false

		for _, prefix := range stackSkipPrefixes {
			if strings.Contains(packageLine, prefix) {
				isNoisy = true
				break
			}
		}

		if !isNoisy {
			startLine = i
			break
		}
	}

	// If all stack frames match the skip prefixes, startLine remains 1.
	// In this case, we return the stack trace starting from line 1 (the error message and all frames).
	return lines[0] + "\n" + strings.Join(lines[startLine:], "\n")
}

// LabelField is a wrapper for the Label function, maintained for backwards compatibility.
// It creates a slog.Attr with the given key and value as a label.
//
// Deprecated: Use Label() instead.
func LabelField(
	key string,
	value string,
) slog.Attr {
	return Label(key, value)
}

// StringField is a wrapper for the String function, maintained for backwards compatibility.
// It creates a slog.Attr with the given key and value as a string field.
//
// Deprecated: Use String() instead.
func StringField(
	key string,
	value string,
) slog.Attr {
	return String(key, value)
}

func Label(
	key string,
	value string,
) slog.Attr {
	return slog.Group("logging.googleapis.com/labels", slog.String(key, value))
}

// Labels creates a group for Google Cloud Logging labels.
//
// Arguments must be provided in pairs: the first element of each pair is the key (string),
// and the second is the value (string). The number of arguments must be even.
// Example:
//
//	Labels("user_id", "123", "role", "admin")
//
// This will produce a group with keys "user_id" and "role" and their corresponding string values.
//
// Note: Both key and value must be strings.
func Labels(
	args ...string,
) slog.Attr {
	anyArgs := make([]any, len(args))
	for i, v := range args {
		anyArgs[i] = v
	}

	if len(anyArgs)%2 != 0 {
		anyArgs = append(anyArgs, nil)
	}

	return slog.Group("logging.googleapis.com/labels", anyArgs...)
}

func String(
	key string,
	value string,
) slog.Attr {
	return slog.String(key, value)
}

func Int(
	key string,
	value int,
) slog.Attr {
	return slog.Int(key, value)
}

func Int32(
	key string,
	value int32,
) slog.Attr {
	return slog.Int64(key, int64(value))
}

func Float32(
	key string,
	value float32,
) slog.Attr {
	return slog.Float64(key, float64(value))
}

func Int64(
	key string,
	value int64,
) slog.Attr {
	return slog.Int64(key, value)
}

func Float64(
	key string,
	value float64,
) slog.Attr {
	return slog.Float64(key, value)
}

// Any is a pragmatic catch‑all that delegates to slog.Any.
// Use the typed helpers above when you can for better performance and clarity.
func Any(
	key string,
	value any,
) slog.Attr {
	return slog.Any(key, value)
}

// ErrorField is a wrapper for the Error function, maintained for backwards compatibility.
// It creates a slog.Attr for the provided error.
//
// Deprecated: Use Error() instead.
func ErrorField(err error) slog.Attr {
	return Error(err)
}

func Error(err error) slog.Attr {
	return slog.Any("error", err)
}

func Duration(key string, duration time.Duration) slog.Attr {
	return slog.Duration(key, duration)
}

// ProtoField is a wrapper for the Proto function, maintained for backwards compatibility.
// It creates a slog.Attr for the provided proto.Message.
//
// Deprecated: Use Proto() instead.
func ProtoField(
	key string,
	value proto.Message,
) slog.Attr {
	return Proto(key, value)
}

func Proto(
	key string,
	value proto.Message,
) slog.Attr {
	bytes, err := protojson.Marshal(value)
	if err != nil {
		return Error(err) // what else to do?
	}
	return slog.Any(key, json.RawMessage(bytes))
}
