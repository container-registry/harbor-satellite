package logger

import (
	"context"
	"os"

	"github.com/rs/zerolog"
)

type contextKey string

const loggerKey contextKey = "logger"

// AddLoggerToContext creates a new context with a zerolog logger for stdout adn stderr and sets the global log level.
func AddLoggerToContext(ctx context.Context, logLevel string) context.Context {
	// Set log level to configured value
	switch logLevel {
	case "debug":
		zerolog.SetGlobalLevel(zerolog.DebugLevel)
	case "info":
		zerolog.SetGlobalLevel(zerolog.InfoLevel)
	case "warn":
		zerolog.SetGlobalLevel(zerolog.WarnLevel)
	case "error":
		zerolog.SetGlobalLevel(zerolog.ErrorLevel)
	case "fatal":
		zerolog.SetGlobalLevel(zerolog.FatalLevel)
	case "panic":
		zerolog.SetGlobalLevel(zerolog.PanicLevel)
	default:
		zerolog.SetGlobalLevel(zerolog.InfoLevel)
	}

	logger := zerolog.New(os.Stderr).With().Timestamp().Logger()
	ctx = context.WithValue(ctx, loggerKey, &logger)

	return ctx
}

// FromContext extracts the main logger from the context.
func FromContext(ctx context.Context) *zerolog.Logger {
	logger, ok := ctx.Value(loggerKey).(*zerolog.Logger)
	if !ok {
		// Fallback to a default logger if none is found in the context.
		defaultLogger := zerolog.New(os.Stderr).With().Timestamp().Logger()
		defaultLogger.Error().Msg("Failed to extract logger from context")
		return &defaultLogger
	}
	return logger
}

func SetupLogger(ctx context.Context, logLevel string) *zerolog.Logger{
	ctx = AddLoggerToContext(ctx, logLevel)
	logger := FromContext(ctx)
	return logger
}
