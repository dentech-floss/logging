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
		),
	}
}

func (l *Logger) WithContext(
	ctx context.Context,
	fields ...zapcore.Field,
) *LoggerWithContext {
	if len(fields) > 0 {
		return &LoggerWithContext{l.Logger.WithOptions(zap.Fields(fields...)).Ctx(ctx)}
	} else {
		return &LoggerWithContext{l.Logger.Ctx(ctx)}
	}
}

func LabelField(
	key string,
	value string,
) zapcore.Field {
	return zapdriver.Label(key, value)
}

func StringField(
	key string,
	value string,
) zapcore.Field {
	return zap.String(key, value)
}

func ErrorField(err error) zapcore.Field {
	return zap.Error(err)
}

func ProtoField(
	key string,
	value proto.Message,
) zapcore.Field {
	bytes, err := protojson.Marshal(value)
	if err != nil {
		return ErrorField(err) // what else to do?
	}
	return zap.Any(key, json.RawMessage(bytes))
}
