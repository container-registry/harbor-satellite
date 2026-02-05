//go:build !nospiffe

package spiffe

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"

	"github.com/spiffe/go-spiffe/v2/spiffeid"
)

// contextKey is a custom type for context keys to avoid collisions.
type contextKey string

const (
	// SPIFFEIDKey is the context key for storing the SPIFFE ID.
	SPIFFEIDKey contextKey = "spiffe_id"

	// SatelliteNameKey is the context key for storing the satellite name.
	SatelliteNameKey contextKey = "satellite_name"

	// RegionKey is the context key for storing the region.
	RegionKey contextKey = "region"
)

// AuthMiddleware extracts SPIFFE identity from the TLS connection
// and adds it to the request context.
func AuthMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id, err := ExtractSPIFFEIDFromRequest(r)
		if err != nil {
			log.Printf("SPIFFE auth: no valid SPIFFE ID in request: %v", err)
			next.ServeHTTP(w, r)
			return
		}

		ctx := context.WithValue(r.Context(), SPIFFEIDKey, id)

		name, err := ExtractSatelliteNameFromSPIFFEID(id)
		if err == nil {
			ctx = context.WithValue(ctx, SatelliteNameKey, name)
		}

		region, err := ExtractRegionFromSPIFFEID(id)
		if err == nil {
			ctx = context.WithValue(ctx, RegionKey, region)
		}

		log.Printf("SPIFFE auth: authenticated satellite %s from region %s", name, region)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// RequireSPIFFEAuth is middleware that requires a valid SPIFFE identity.
// Requests without a valid SPIFFE ID are rejected with 401 Unauthorized.
func RequireSPIFFEAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id, err := ExtractSPIFFEIDFromRequest(r)
		if err != nil {
			log.Printf("SPIFFE auth required: no valid SPIFFE ID: %v", err)
			writeJSONResponse(w, http.StatusUnauthorized, map[string]string{"error": "SPIFFE authentication required"})
			return
		}

		ctx := context.WithValue(r.Context(), SPIFFEIDKey, id)

		name, err := ExtractSatelliteNameFromSPIFFEID(id)
		if err == nil {
			ctx = context.WithValue(ctx, SatelliteNameKey, name)
		}

		region, err := ExtractRegionFromSPIFFEID(id)
		if err == nil {
			ctx = context.WithValue(ctx, RegionKey, region)
		}

		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// GetSPIFFEID retrieves the SPIFFE ID from the request context.
func GetSPIFFEID(ctx context.Context) (spiffeid.ID, bool) {
	id, ok := ctx.Value(SPIFFEIDKey).(spiffeid.ID)
	return id, ok
}

// GetSatelliteName retrieves the satellite name from the request context.
func GetSatelliteName(ctx context.Context) (string, bool) {
	name, ok := ctx.Value(SatelliteNameKey).(string)
	return name, ok
}

// GetRegion retrieves the region from the request context.
func GetRegion(ctx context.Context) (string, bool) {
	region, ok := ctx.Value(RegionKey).(string)
	return region, ok
}

// TokenAuthMiddleware handles token-based authentication for satellites.
// Use this for non-SPIFFE deployments where satellites authenticate via tokens.
type TokenAuthMiddleware struct {
	tokenValidator func(token string) (int64, error)
}

// NewTokenAuthMiddleware creates middleware for token-based authentication.
func NewTokenAuthMiddleware() *TokenAuthMiddleware {
	return &TokenAuthMiddleware{}
}

// SetTokenValidator sets the function used to validate tokens.
func (m *TokenAuthMiddleware) SetTokenValidator(validator func(token string) (int64, error)) {
	m.tokenValidator = validator
}

// Wrap wraps a handler with token authentication.
// Rejects requests without a valid token.
func (m *TokenAuthMiddleware) Wrap(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if m.tokenValidator == nil {
			writeJSONResponse(w, http.StatusInternalServerError, map[string]string{"error": "token validator not configured"})
			return
		}

		token := r.Header.Get("Authorization")
		if token == "" {
			writeJSONResponse(w, http.StatusUnauthorized, map[string]string{"error": "authorization token required"})
			return
		}

		satelliteID, err := m.tokenValidator(token)
		if err != nil {
			log.Printf("Token auth failed: %v", err)
			writeJSONResponse(w, http.StatusUnauthorized, map[string]string{"error": "invalid token"})
			return
		}

		ctx := context.WithValue(r.Context(), SatelliteNameKey, fmt.Sprintf("satellite-%d", satelliteID))
		log.Printf("Authenticated via token: satellite-%d", satelliteID)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func writeJSONResponse(w http.ResponseWriter, statusCode int, data any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(statusCode)
	_ = json.NewEncoder(w).Encode(data)
}
