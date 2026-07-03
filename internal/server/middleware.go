package server

import (
	"log"
	"net/http"
	"strings"
	"time"
)

type Middleware func(http.Handler) http.Handler

// LoggingMiddleware creates a new logging middleware.
func LoggingMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		recorder := &statusRecorder{ResponseWriter: w, status: http.StatusOK}

		// Call the next handler
		next.ServeHTTP(recorder, r)

		// Log the request
		log.Printf( //nolint:gosec // Request-derived fields are sanitized before logging.
			"remote=%q method=%q path=%q status=%d duration=%s",
			sanitizeLogField(r.RemoteAddr),
			sanitizeLogField(r.Method),
			sanitizeLogField(r.URL.Path),
			recorder.status,
			time.Since(start),
		)
	})
}

type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (r *statusRecorder) WriteHeader(status int) {
	r.status = status
	r.ResponseWriter.WriteHeader(status)
}

func sanitizeLogField(value string) string {
	return strings.Map(func(char rune) rune {
		switch char {
		case '\r', '\n':
			return -1
		default:
			return char
		}
	}, value)
}
