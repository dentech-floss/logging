package logging

import "github.com/ThreeDotsLabs/watermill"

type WatermillAdapter struct {
	l *Logger
}

func NewWatermillAdapter(l *Logger) watermill.LoggerAdapter {
	return &WatermillAdapter{l: l}
}

func (wa *WatermillAdapter) Error(msg string, err error, fields watermill.LogFields) {
	wa.l.Error(msg, append(slogAttrsFromFields(fields), Error(err))...)
}

func (wa *WatermillAdapter) Info(msg string, fields watermill.LogFields) {
	wa.l.Info(msg, slogAttrsFromFields(fields)...)
}

func (wa *WatermillAdapter) Debug(msg string, fields watermill.LogFields) {
	wa.l.Debug(msg, slogAttrsFromFields(fields)...)
}

func (wa *WatermillAdapter) Trace(msg string, fields watermill.LogFields) {
	wa.l.Info(msg, slogAttrsFromFields(fields)...)
}

func (wa *WatermillAdapter) With(fields watermill.LogFields) watermill.LoggerAdapter {
	withLogger := wa.l.With(slogAttrsFromFields(fields)...)

	return &WatermillAdapter{l: withLogger}
}

func slogAttrsFromFields(fields watermill.LogFields) []any {
	result := make([]any, 0, len(fields)*2)

	for key, value := range fields {
		result = append(result, key, value)
	}

	return result
}
