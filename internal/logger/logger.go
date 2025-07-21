package logger

import (
	"github.com/yudaprama/grid-trading-bot/internal/models"
	"os"
	"strings"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"gopkg.in/natefinch/lumberjack.v2"
)

var (
	logger        *zap.Logger
	sugaredLogger *zap.SugaredLogger
)

// InitLogger initialize the zap logger
func InitLogger(cfg models.LogConfig) {
	// Configure the log level
	logLevel := zap.NewAtomicLevel()
	if err := logLevel.UnmarshalText([]byte(cfg.Level)); err != nil {
		logLevel.SetLevel(zap.InfoLevel) // Default to Info level
	}

	// Configure the encoder
	encoderConfig := zap.NewProductionEncoderConfig()
	encoderConfig.EncodeTime = zapcore.ISO8601TimeEncoder
	// Enable color for console output
	encoderConfig.EncodeLevel = zapcore.CapitalColorLevelEncoder
	consoleEncoder := zapcore.NewConsoleEncoder(encoderConfig)

	// Create cores based on the configuration
	var cores []zapcore.Core

	output := strings.ToLower(cfg.Output)
	if output == "file" || output == "both" {
		// Set lumberjack for log cutting
		lumberjackLogger := &lumberjack.Logger{
			Filename:   cfg.File,
			MaxSize:    cfg.MaxSize,
			MaxBackups: cfg.MaxBackups,
			MaxAge:     cfg.MaxAge,
			Compress:   cfg.Compress,
		}
		fileWriter := zapcore.AddSync(lumberjackLogger)
		cores = append(cores, zapcore.NewCore(consoleEncoder, fileWriter, logLevel))
	}

	if output == "console" || output == "both" {
		// Setup console output
		consoleWriter := zapcore.AddSync(os.Stdout)
		cores = append(cores, zapcore.NewCore(consoleEncoder, consoleWriter, logLevel))
	}

	// If no valid core exists (e.g., configuration error), default to console output
	if len(cores) == 0 {
		consoleWriter := zapcore.AddSync(os.Stdout)
		cores = append(cores, zapcore.NewCore(consoleEncoder, consoleWriter, logLevel))
	}

	// Create Tee Core
	core := zapcore.NewTee(cores...)

	// Create logger
	logger = zap.New(core, zap.AddCaller())
	sugaredLogger = logger.Sugar()
}

// S returns the global sugared logger instance
func S() *zap.SugaredLogger {
	if sugaredLogger == nil {
		// If logger is not initialized, provide a default emergency logger
		defaultLogger, _ := zap.NewDevelopment()
		return defaultLogger.Sugar()
	}
	return sugaredLogger
}

// L returns the global logger instance
func L() *zap.Logger {
	if logger == nil {
		// If the logger is not initialized, provide a default emergency logger
		defaultLogger, _ := zap.NewDevelopment()
		return defaultLogger
	}
	return logger
}
