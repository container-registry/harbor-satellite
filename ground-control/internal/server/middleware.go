package server

import (
	"context"
	"encoding/base64"
	"log"
	"net/http"
	"strings"

	"github.com/container-registry/harbor-satellite/ground-control/internal/auth"
	"github.com/container-registry/harbor-satellite/ground-control/internal/spiffe"
	"github.com/container-registry/harbor-satellite/ground-control/pkg/crypto"
)

type contextKey string

const (
	userContextKey   contextKey = "user"
	satelliteNameKey contextKey = "satellite_name"
	satelliteIDKey   contextKey = "satellite_id"
)

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

// SatelliteAuthMiddleware authenticates satellite requests using dual auth:
// 1. SPIFFE/mTLS - identity extracted from TLS client certificate
// 2. Robot Account Basic Auth - credentials from ZTR stored in DB
// Returns 401 if neither authentication method succeeds.
func (s *Server) SatelliteAuthMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var satelliteName string
		var satelliteID int32
		var authenticated bool

		// 1. Try SPIFFE first (mTLS) - check if context already has SPIFFE identity
		if name, ok := spiffe.GetSatelliteName(r.Context()); ok {
			satelliteName = name
			// Look up satellite to get ID
			sat, err := s.dbQueries.GetSatelliteByName(r.Context(), name)
			if err == nil {
				satelliteID = sat.ID
				authenticated = true
				log.Printf("Satellite %s authenticated via SPIFFE mTLS", name)
			} else {
				log.Printf("SPIFFE auth: satellite %s not found in database", name)
			}
		}

		// Also check raw SPIFFE ID if satellite name not directly available
		if !authenticated {
			if id, ok := spiffe.GetSPIFFEID(r.Context()); ok {
				name, err := spiffe.ExtractSatelliteNameFromSPIFFEID(id)
				if err == nil {
					sat, err := s.dbQueries.GetSatelliteByName(r.Context(), name)
					if err == nil {
						satelliteName = name
						satelliteID = sat.ID
						authenticated = true
						log.Printf("Satellite %s authenticated via SPIFFE ID", name)
					}
				}
			}
		}

		// 2. Try Robot Account credentials (Basic Auth) if SPIFFE didn't work
		if !authenticated {
			username, password, ok := extractBasicAuth(r)
			if ok {
				robot, err := s.dbQueries.GetRobotAccByName(r.Context(), username)
				if err == nil {
					if crypto.VerifySecret(password, robot.RobotSecretHash) {
						sat, err := s.dbQueries.GetSatellite(r.Context(), robot.SatelliteID)
						if err == nil {
							satelliteName = sat.Name
							satelliteID = sat.ID
							authenticated = true
							log.Printf("Satellite %s authenticated via robot credentials", sat.Name)
						}
					} else {
						log.Printf("Satellite auth failed: invalid robot credentials for %s", username)
					}
				} else {
					log.Printf("Satellite auth failed: robot account not found: %s", username)
				}
			}
		}

		// 3. No valid auth - reject
		if !authenticated {
			log.Printf("Satellite auth failed: no valid authentication provided from %s", r.RemoteAddr)
			WriteJSONError(w, "satellite authentication required", http.StatusUnauthorized)
			return
		}

		// Add satellite info to context
		ctx := context.WithValue(r.Context(), satelliteNameKey, satelliteName)
		ctx = context.WithValue(ctx, satelliteIDKey, satelliteID)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// GetSatelliteNameFromContext extracts the authenticated satellite name from the request context.
func GetSatelliteNameFromContext(ctx context.Context) (string, bool) {
	name, ok := ctx.Value(satelliteNameKey).(string)
	return name, ok
}

// GetSatelliteIDFromContext extracts the authenticated satellite ID from the request context.
func GetSatelliteIDFromContext(ctx context.Context) (int32, bool) {
	id, ok := ctx.Value(satelliteIDKey).(int32)
	return id, ok
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
