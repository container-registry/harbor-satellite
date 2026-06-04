package server

import (
	"context"
	"encoding/base64"
	"net/http"
	"strings"
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
		var principal apiPrincipal

		if token := extractBearerToken(r); token != "" {
			resolvedPrincipal, err := s.authenticateBearer(r.Context(), token)
			if err == nil {
				principal = resolvedPrincipal
			}
		}

		if principal.User.Username == "" {
			if username, password, ok := extractBasicAuth(r); ok {
				resolvedPrincipal, err := s.authenticateBasic(r.Context(), username, password)
				if err == nil {
					principal = resolvedPrincipal
				}
			}
		}

		if principal.User.Username == "" {
			WriteJSONError(w, "Unauthorized", http.StatusUnauthorized)
			return
		}

		ctx := context.WithValue(r.Context(), userContextKey, principal.User)
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
