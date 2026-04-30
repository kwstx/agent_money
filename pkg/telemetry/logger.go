package telemetry

import (
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"os"
)

var Logger *zap.Logger

func InitLogger(serviceName string) {
	config := zap.NewProductionConfig()
	config.EncoderConfig.TimeKey = "timestamp"
	config.EncoderConfig.EncodeTime = zapcore.ISO8601TimeEncoder
	
	var err error
	Logger, err = config.Build(zap.Fields(
		zap.String("service", serviceName),
	))
	if err != nil {
		panic(err)
	}
	zap.ReplaceGlobals(Logger)
}

func GetLogger() *zap.Logger {
	if Logger == nil {
		InitLogger("default")
	}
	return Logger
}
