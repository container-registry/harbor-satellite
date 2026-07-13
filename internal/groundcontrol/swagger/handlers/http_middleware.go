package handlers

import (
	"encoding/json"
	"log"
	"net/http"
	"strings"

	auditlog "github.com/container-registry/harbor-satellite/internal/groundcontrol/logger"
	swaggermodels "github.com/container-registry/harbor-satellite/internal/groundcontrol/swagger/models"
	"github.com/google/uuid"
)

// RequestIDMiddleware gives every request a safe correlation ID, echoes it to
// the caller, and makes it available to handler audit events through the
// request header.
func RequestIDMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestID := strings.TrimSpace(r.Header.Get("X-Request-ID"))
		if !validAuditRequestID(requestID) {
			requestID = uuid.NewString()
		}

		r.Header.Set("X-Request-ID", requestID)
		w.Header().Set("X-Request-ID", requestID)
		next.ServeHTTP(w, r)
	})
}

// RateLimitMiddleware constructs route-scoped throttling with a structured
// audit event when a request is rejected. A shared limiter preserves the old
// Ground Control behavior of a single per-IP budget across login and ZTR.
func RateLimitMiddleware(operation auditlog.Operation, resourceType auditlog.ResourceType, flow string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			svc, err := getService()
			if err != nil || svc.rateLimiter == nil {
				next.ServeHTTP(w, r)
				return
			}

			if svc.rateLimiter.Allow(clientIP(r, svc.trustForwardedHeaders)) {
				next.ServeHTTP(w, r)
				return
			}

			var details map[string]any
			if flow != "" {
				details = map[string]any{"flow": flow}
			}
			svc.auditEvent(r, auditlog.AuditEvent{
				Operation:    operation,
				ResourceType: resourceType,
				Outcome:      auditlog.OutcomeFailure,
				ActorType:    auditlog.ActorAnonymous,
				Reason:       auditlog.ReasonRateLimited,
				Details:      details,
			})

			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusTooManyRequests)
			if err := json.NewEncoder(w).Encode(&swaggermodels.AppError{
				Code:    http.StatusTooManyRequests,
				Message: "Too many requests",
			}); err != nil {
				log.Printf("failed to write rate-limit response: %v", err)
			}
		})
	}
}
