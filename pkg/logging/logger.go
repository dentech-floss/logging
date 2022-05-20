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

type Logger struct {
	*otelzap.Logger
}

type LoggerWithContext struct {
	otelzap.LoggerWithCtx
}

type LoggerConfig struct {
	OnGCP       bool
	ServiceName string
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
			otelzap.WithMinLevel(zap.InfoLevel),
			otelzap.WithTraceIDField(true),
		),
	}
}

func (l *Logger) WithContext(ctx context.Context, fields ...zapcore.Field) *LoggerWithContext {
	if len(fields) > 0 {
		return &LoggerWithContext{l.Logger.WithOptions(zap.Fields(fields...)).Ctx(ctx)}
	} else {
		return &LoggerWithContext{l.Logger.Ctx(ctx)}
	}
}

func LabelField(key string, value string) zapcore.Field {
	return zapdriver.Label(key, value)
}

func StringField(key string, value string) zapcore.Field {
	return zap.String(key, value)
}

func ErrorField(err error) zapcore.Field {
	return zap.Error(err)
}

func ProtoField(key string, value proto.Message) zapcore.Field {
	bytes, err := protojson.Marshal(value)
	if err != nil {
		return ErrorField(err) // what else to do?
	}
	return zap.Any(key, json.RawMessage(bytes))
}
