package logger

import (
	"context"
	"os"

	"github.com/rs/zerolog"
)

type contextKey string

const loggerKey contextKey = "logger"
const errorLoggerKey contextKey = "errorLogger"

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
	// Use os.Stdout for the main logger
	logger := zerolog.New(os.Stdout).With().Timestamp().Logger()
	ctx = context.WithValue(ctx, loggerKey, &logger)

	// Use os.Stderr for the error logger
	errorLogger := zerolog.New(os.Stderr).With().Timestamp().Logger()
	ctx = context.WithValue(ctx, errorLoggerKey, &errorLogger)

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

// ErrorLoggerFromContext extracts the error logger from the context.
func ErrorLoggerFromContext(ctx context.Context) *zerolog.Logger {
	errorLogger, ok := ctx.Value(errorLoggerKey).(*zerolog.Logger)
	if !ok {
		// Fallback to a default logger if none is found in the context.
		defaultErrorLogger := zerolog.New(os.Stderr).With().Timestamp().Logger()
		defaultErrorLogger.Error().Msg("Failed to extract error logger from context")
		return &defaultErrorLogger
	}
	return errorLogger
}
