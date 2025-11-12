package logging

import (
	"context"
	"errors"
	"fmt"
	"time"

	"gorm.io/gorm"
	gormlogger "gorm.io/gorm/logger"
)

// GormLogger - Structure to implement "gorm.io/gorm/logger".Interface
type GormLogger struct {
	Logger                    *Logger
	LogLevel                  gormlogger.LogLevel
	SlowThreshold             time.Duration // Slow SQL threshold
	IgnoreRecordNotFoundError bool
}

func NewGormLogger(logger *Logger) *GormLogger {
	return &GormLogger{
		Logger:                    logger,
		LogLevel:                  gormlogger.Warn,
		SlowThreshold:             200 * time.Millisecond,
		IgnoreRecordNotFoundError: true,
	}
}

func (l *GormLogger) LogMode(level gormlogger.LogLevel) gormlogger.Interface {
	return &GormLogger{
		Logger:                    l.Logger,
		LogLevel:                  level,
		SlowThreshold:             l.SlowThreshold,
		IgnoreRecordNotFoundError: l.IgnoreRecordNotFoundError,
	}
}

func (l *GormLogger) Info(ctx context.Context, str string, args ...interface{}) {
	if l.LogLevel < gormlogger.Info {
		return
	}

	l.Logger.DebugContext(ctx, fmt.Sprintf(str, args...))
}

func (l *GormLogger) Warn(ctx context.Context, str string, args ...interface{}) {
	if l.LogLevel < gormlogger.Warn {
		return
	}

	l.Logger.WarnContext(ctx, fmt.Sprintf(str, args...))
}

func (l *GormLogger) Error(ctx context.Context, str string, args ...interface{}) {
	if l.LogLevel < gormlogger.Error {
		return
	}
	l.Logger.ErrorContext(ctx, fmt.Sprintf(str, args...))
}

func (l *GormLogger) Trace(ctx context.Context, begin time.Time, fc func() (string, int64), err error) {
	if l.LogLevel <= 0 {
		return
	}
	elapsed := time.Since(begin)

	switch {
	case err != nil &&
		l.LogLevel >= gormlogger.Error &&
		(!l.IgnoreRecordNotFoundError || !errors.Is(err, gorm.ErrRecordNotFound)):
		sql, rows := fc()
		l.Logger.ErrorContext(
			ctx,
			"sql error trace",
			Error(err),
			Duration(elapsed),
			Int64("rows", rows),
			String("sql", sql),
		)
	case l.SlowThreshold != 0 && elapsed > l.SlowThreshold && l.LogLevel >= gormlogger.Warn:
		sql, rows := fc()
		l.Logger.WarnContext(
			ctx,
			"sql slow query trace",
			Duration(elapsed),
			Int64("rows", rows),
			String("sql", sql),
		)
	case l.LogLevel >= gormlogger.Info:
		sql, rows := fc()
		l.Logger.DebugContext(
			ctx,
			"sql debug trace",
			Duration(elapsed),
			Int64("rows", rows),
			String("sql", sql),
		)
	}
}
