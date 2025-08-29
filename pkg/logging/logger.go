// Package logging provides a thin abstraction layer around zap/otelzap.
//
// Why wrap zap fields?
//
// We intentionally provide helpers like String, Int, etc. even
// though they currently delegate directly to zap.String, zap.Int, etc.
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

	"github.com/blendle/zapdriver"
	"github.com/uptrace/opentelemetry-go-extra/otelzap"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"

	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
)

// A Level is a logging priority. Higher levels are more important.
type Level int8

const (
	// DebugLevel logs are typically voluminous, and are usually disabled in
	// production.
	DebugLevel Level = iota - 1
	// InfoLevel is the default logging priority.
	InfoLevel
	// WarnLevel logs are more important than Info, but don't need individual
	// human review.
	WarnLevel
	// ErrorLevel logs are high-priority. If an application is running smoothly,
	// it shouldn't generate any error-level logs.
	ErrorLevel
	// DPanicLevel logs are particularly important errors. In development the
	// logger panics after writing the message.
	DPanicLevel
	// PanicLevel logs a message, then panics.
	PanicLevel
	// FatalLevel logs a message, then calls os.Exit(1).
	FatalLevel
)

type Logger struct {
	*otelzap.Logger
}

type LoggerWithContext struct {
	otelzap.LoggerWithCtx
}

type LoggerConfig struct {
	OnGCP       bool
	ServiceName string
	MinLevel    Level
}

func NewLogger(config *LoggerConfig) *Logger {

	var log *zap.Logger
	var err error

	if config.OnGCP {
		// https://github.com/blendle/zapdriver#using-error-reporting
		log, err = zapdriver.NewProductionWithCore(
			zapdriver.WrapCore(
				zapdriver.ReportAllErrors(true),
				zapdriver.ServiceName(config.ServiceName),
			),
		)
	} else {
		log, err = zapdriver.NewDevelopment()
	}
	if err != nil {
		panic(err)
	}

	return &Logger{
		// instrumentation
		otelzap.New(
			log,
			otelzap.WithMinLevel(zapcore.Level(config.MinLevel)),
			otelzap.WithTraceIDField(true),
		),
	}
}

func (l *Logger) WithContext(
	ctx context.Context,
	fields ...zapcore.Field,
) *LoggerWithContext {
	if len(fields) > 0 {
		return &LoggerWithContext{l.Logger.WithOptions(zap.Fields(fields...)).Ctx(ctx)}
	}

	return &LoggerWithContext{l.Logger.Ctx(ctx)}
}

func (l *Logger) With(fields ...zapcore.Field) *Logger {
	return &Logger{l.Logger.WithOptions(zap.Fields(fields...))}
}

func (lc *LoggerWithContext) With(fields ...zapcore.Field) *LoggerWithContext {
	return &LoggerWithContext{lc.LoggerWithCtx.WithOptions(zap.Fields(fields...))}
}

// LabelField - kept for backwards compatibility. Use Label() instead.
func LabelField(
	key string,
	value string,
) zapcore.Field {
	return Label(key, value)
}

// StringField - kept for backwards compatibility. Use String() instead.
func StringField(
	key string,
	value string,
) zapcore.Field {
	return String(key, value)
}

func Label(
	key string,
	value string,
) zapcore.Field {
	return zapdriver.Label(key, value)
}

func String(
	key string,
	value string,
) zapcore.Field {
	return zap.String(key, value)
}

func Int(
	key string,
	value int,
) zapcore.Field {
	return zap.Int(key, value)
}

func Int32(
	key string,
	value int32,
) zapcore.Field {
	return zap.Int32(key, value)
}

func Float32(
	key string,
	value float32,
) zapcore.Field {
	return zap.Float32(key, value)
}

func Int64(
	key string,
	value int64,
) zapcore.Field {
	return zap.Int64(key, value)
}

func Float64(
	key string,
	value float64,
) zapcore.Field {
	return zap.Float64(key, value)
}

// Any is a pragmatic catch‑all that delegates to zap.Any.
// Use the typed helpers above when you can for better performance and clarity.
func Any(
	key string,
	value any,
) zapcore.Field {
	return zap.Any(key, value)
}

// ErrorField - kept for backwards compatibility. Use Error() instead.
func ErrorField(err error) zapcore.Field {
	return Error(err)
}

func Error(err error) zapcore.Field {
	return zap.Error(err)
}

// ProtoField - kept for backwards compatibility. Use Proto() instead.
func ProtoField(
	key string,
	value proto.Message,
) zapcore.Field {
	return Proto(key, value)
}

func Proto(
	key string,
	value proto.Message,
) zapcore.Field {
	bytes, err := protojson.Marshal(value)
	if err != nil {
		return ErrorField(err) // what else to do?
	}
	return zap.Any(key, json.RawMessage(bytes))
}
