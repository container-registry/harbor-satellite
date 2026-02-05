package server

import (
	"context"
	"encoding/base64"
	"net/http"
	"strings"

	"github.com/container-registry/harbor-satellite/ground-control/internal/auth"
	auditLogger "github.com/container-registry/harbor-satellite/ground-control/internal/logger"
)

type contextKey string

const userContextKey contextKey = "user"

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
		var actor string

		// Try Bearer token first
		if token := extractBearerToken(r); token != "" {
			session, err := s.dbQueries.GetSessionByToken(r.Context(), token)
			if err == nil {
				user = AuthUser{
					ID:       session.UserID,
					Username: session.Username,
					Role:     session.Role,
				}
				actor = session.Username
				authenticated = true
			}
		}

		// Try Basic auth if Bearer didn't work
		if !authenticated {
			if username, password, ok := extractBasicAuth(r); ok {
				dbUser, err := s.dbQueries.GetUserByUsername(r.Context(), username)
				if err == nil {
					valid, err := auth.VerifyPassword(password, dbUser.PasswordHash)
					if err == nil && valid {
						user = AuthUser{
							ID:       dbUser.ID,
							Username: dbUser.Username,
							Role:     dbUser.Role,
						}
						actor = dbUser.Username
						authenticated = true
					}
				}
			}
		}

		if !authenticated {
			auditLogger.LogEvent(r.Context(), "user.auth.failure", actor, auditLogger.ClientIP(r), map[string]interface{}{
				"reason": "invalid_credentials",
				"path":   r.URL.Path,
				"method": r.Method,
			})
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
