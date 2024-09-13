package server

import (
	"log"
	"net/http"
	"time"
)

type Middleware func(http.Handler) http.Handler

// LoggingMiddleware creates a new logging middleware
func LoggingMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()

		// Call the next handler
		next.ServeHTTP(w, r)

		// Log the request
		log.Printf(
			"%s %s %s %s %s",
			r.RemoteAddr,
			r.Method,
			r.URL.Path,
			r.Response.Status,
			time.Since(start),
		)
	})
}
