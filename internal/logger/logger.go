package logger

import "go.uber.org/zap"

type Logger struct {
	logger *zap.Logger
}

func NewLogger(zapLogger *zap.Logger) Logger {
	return Logger{logger: zapLogger}
}

func (l Logger) Log(v ...interface{}) {
	l.logger.Sugar().Info(v...)
}

func (l Logger) Logf(format string, v ...interface{}) {
	l.logger.Sugar().Infof(format, v...)
}
