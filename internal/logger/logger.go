package logger

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/container-registry/harbor-satellite/internal/utils"
	"github.com/rs/zerolog"
)

type contextKey string

const LoggerKey contextKey = "logger"

func InitLogger(ctx context.Context, logLevel string, warnings []string) (context.Context, *zerolog.Logger) {
	log := NewLogger(logLevel)
	utils.HandleWarnings(log, warnings)
	ctx = context.WithValue(ctx, LoggerKey, log)
	return ctx, log
}

// AddLoggerToContext creates a new context with a zerolog logger for stdout and stderr and sets the global log level.
func NewLogger(logLevel string) *zerolog.Logger {
	// Set log level to configured value
	level := getLogLevel(logLevel)
	zerolog.SetGlobalLevel(level)

	// Create a custom console writer
	output := zerolog.ConsoleWriter{Out: os.Stderr, TimeFormat: "2006-01-02 15:04:05"}

	// Customize the output for each log level
	output.FormatLevel = func(i interface{}) string {
		var l string
		if ll, ok := i.(string); ok {
			switch ll {
			case "debug":
				l = colorize(ll, 36) // cyan
			case "info":
				l = colorize(ll, 34) // blue
			case "warn":
				l = colorize(ll, 33) // yellow
			case "error":
				l = colorize(ll, 31) // red
			case "fatal":
				l = colorize(ll, 35) // magenta
			case "panic":
				l = colorize(ll, 41) // white on red background
			default:
				l = colorize(ll, 37) // white
			}
		} else {
			if i == nil {
				l = colorize("???", 37) // white
			} else {
				lStr := strings.ToUpper(fmt.Sprintf("%s", i))
				if len(lStr) > 3 {
					lStr = lStr[:3]
				}
				l = lStr
			}
		}
		return fmt.Sprintf("| %s |", l)
	}

	logger := zerolog.New(output).With().Timestamp().Logger()

	return &logger
}

// FromContext extracts the main logger from the context.
func FromContext(ctx context.Context) *zerolog.Logger {
	logger, ok := ctx.Value(LoggerKey).(*zerolog.Logger)
	if !ok {
		// Fallback to a default logger if none is found in the context.
		defaultLogger := zerolog.New(os.Stderr).With().Timestamp().Logger()
		defaultLogger.Error().Msg("Failed to extract logger from context")
		return &defaultLogger
	}
	return logger
}

// Helper function to get the log level
func getLogLevel(logLevel string) zerolog.Level {
	switch logLevel {
	case "debug":
		return zerolog.DebugLevel
	case "info":
		return zerolog.InfoLevel
	case "warn":
		return zerolog.WarnLevel
	case "error":
		return zerolog.ErrorLevel
	case "fatal":
		return zerolog.FatalLevel
	case "panic":
		return zerolog.PanicLevel
	default:
		return zerolog.InfoLevel
	}
}

// Helper function to colorize text
func colorize(s string, color int) string {
	return fmt.Sprintf("\x1b[%dm%s\x1b[0m", color, s)
}
