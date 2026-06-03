package server

import (
	"context"
	"encoding/base64"
	"net"
	"net/http"
	"strings"

	"github.com/container-registry/harbor-satellite/ground-control/internal/auth"
	auditlog "github.com/container-registry/harbor-satellite/ground-control/internal/logger"
	"github.com/google/uuid"
)

type contextKey string

const userContextKey contextKey = "user"

const requestIDContextKey contextKey = "request_id"

const maxRequestIDLen = 128

// validRequestID reports whether s is a safe correlation ID: 1..128 characters
// of [A-Za-z0-9._-]. The inbound X-Request-ID is client-controlled and flows
// into the audit log, so values that fail this check are discarded in favour of
// a generated UUID — a caller cannot inject oversized or adversarial strings
// into the audit trail.
func validRequestID(s string) bool {
	const allowed = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789._-"
	if len(s) == 0 || len(s) > maxRequestIDLen {
		return false
	}
	for _, c := range s {
		if !strings.ContainsRune(allowed, c) {
			return false
		}
	}
	return true
}

// RequestIDMiddleware ensures every request carries a request ID used to
// correlate the audit events it produces. A well-formed inbound X-Request-ID is
// reused; anything else (absent, oversized, or containing unexpected
// characters) is replaced by a generated UUID. The value is stored on the
// request context and echoed on the response so clients can correlate too.
func (s *Server) RequestIDMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rid := strings.TrimSpace(r.Header.Get("X-Request-ID"))
		if !validRequestID(rid) {
			rid = uuid.NewString()
		}
		w.Header().Set("X-Request-ID", rid)
		ctx := context.WithValue(r.Context(), requestIDContextKey, rid)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// requestIDFromContext returns the request ID attached by RequestIDMiddleware,
// or "" if none is set.
func requestIDFromContext(ctx context.Context) string {
	if rid, ok := ctx.Value(requestIDContextKey).(string); ok {
		return rid
	}
	return ""
}

// AuthUser represents the authenticated user in the request context
type AuthUser struct {
	ID       int32
	Username string
	Role     string
}

// AuthMiddleware validates session tokens or basic auth and adds user info to context
func (s *Server) AuthMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var user AuthUser
		var authenticated bool

		// Try Bearer token first
		if token := extractBearerToken(r); token != "" {
			session, err := s.dbQueries.GetSessionByToken(r.Context(), token)
			if err == nil {
				user = AuthUser{
					ID:       session.UserID,
					Username: session.Username,
					Role:     session.Role,
				}
				authenticated = true
			}
		}

		// Try Basic auth if Bearer didn't work
		if !authenticated {
			if username, password, ok := extractBasicAuth(r); ok {
				dbUser, err := s.dbQueries.GetUserByUsername(r.Context(), username)
				if err == nil {
					valid := auth.VerifyPassword(password, dbUser.PasswordHash)
					if valid {
						user = AuthUser{
							ID:       dbUser.ID,
							Username: dbUser.Username,
							Role:     dbUser.Role,
						}
						authenticated = true
					}
				}
			}
		}

		if !authenticated {
			WriteJSONError(w, "Unauthorized", http.StatusUnauthorized)
			return
		}

		ctx := context.WithValue(r.Context(), userContextKey, user)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// RequireRole creates middleware that checks if user has the required role
func (s *Server) RequireRole(role string, next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user, ok := GetUserFromContext(r.Context())
		if !ok {
			WriteJSONError(w, "Unauthorized", http.StatusUnauthorized)
			return
		}

		if user.Role != role {
			WriteJSONError(w, "Forbidden", http.StatusForbidden)
			return
		}

		next(w, r)
	}
}

// GetUserFromContext extracts the authenticated user from the request context
func GetUserFromContext(ctx context.Context) (AuthUser, bool) {
	user, ok := ctx.Value(userContextKey).(AuthUser)
	return user, ok
}

func extractToken(r *http.Request) string {
	return extractBearerToken(r)
}

func extractBearerToken(r *http.Request) string {
	authHeader := r.Header.Get("Authorization")
	if authHeader == "" {
		return ""
	}

	parts := strings.SplitN(authHeader, " ", 2)
	if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") {
		return ""
	}

	return parts[1]
}

// clientIP returns the request's source IP. By default it returns the host
// portion of RemoteAddr, ignoring X-Forwarded-For and X-Real-IP because those
// headers can be set by any client and would make audit source_ip values
// unreliable. When s.trustForwardedHeaders is true (i.e. GC is behind a
// trusted reverse proxy), the first entry of X-Forwarded-For is used, then
// X-Real-IP, then RemoteAddr.
func (s *Server) clientIP(r *http.Request) string {
	if s != nil && s.trustForwardedHeaders {
		if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
			if comma := strings.Index(xff, ","); comma > 0 {
				return strings.TrimSpace(xff[:comma])
			}
			return strings.TrimSpace(xff)
		}
		if xri := strings.TrimSpace(r.Header.Get("X-Real-IP")); xri != "" {
			return xri
		}
	}
	if host, _, err := net.SplitHostPort(r.RemoteAddr); err == nil {
		return host
	}
	return r.RemoteAddr
}

// auditEvent records a security-relevant event, filling in the request-scoped
// fields (source IP, user agent, request ID) from r before emitting. Callers
// supply the semantic fields on e. It is safe to call when the audit logger is
// disabled.
func (s *Server) auditEvent(r *http.Request, e auditlog.AuditEvent) {
	e.SourceIP = s.clientIP(r)
	if ua := r.UserAgent(); ua != "" {
		e.UserAgent = ua
	}
	if rid := requestIDFromContext(r.Context()); rid != "" {
		e.RequestID = rid
	}
	s.audit.Log(e)
}

func extractBasicAuth(r *http.Request) (username, password string, ok bool) {
	authHeader := r.Header.Get("Authorization")
	if authHeader == "" {
		return "", "", false
	}

	parts := strings.SplitN(authHeader, " ", 2)
	if len(parts) != 2 || !strings.EqualFold(parts[0], "Basic") {
		return "", "", false
	}

	decoded, err := base64.StdEncoding.DecodeString(parts[1])
	if err != nil {
		return "", "", false
	}

	credentials := strings.SplitN(string(decoded), ":", 2)
	if len(credentials) != 2 {
		return "", "", false
	}

	return credentials[0], credentials[1], true
}
