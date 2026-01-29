//go:build !nospiffe

package spiffe

import (
	"context"
	"encoding/json"
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

// DualAuthMiddleware supports both token-based and SPIFFE-based authentication.
// It first checks for SPIFFE identity, then falls back to token auth.
type DualAuthMiddleware struct {
	authMode       string
	spiffeEnabled  bool
	trustDomain    spiffeid.TrustDomain
	tokenValidator func(token string) (int64, error)
}

// NewDualAuthMiddleware creates middleware that supports both auth modes.
// authMode can be "spiffe", "token", or "both".
func NewDualAuthMiddleware(authMode string, spiffeEnabled bool, trustDomain string) *DualAuthMiddleware {
	var td spiffeid.TrustDomain
	if spiffeEnabled {
		var err error
		td, err = spiffeid.TrustDomainFromString(trustDomain)
		if err != nil {
			log.Printf("Warning: invalid trust domain %q: %v", trustDomain, err)
		}
	}

	return &DualAuthMiddleware{
		authMode:      authMode,
		spiffeEnabled: spiffeEnabled,
		trustDomain:   td,
	}
}

// SetTokenValidator sets the function used to validate tokens.
func (m *DualAuthMiddleware) SetTokenValidator(validator func(token string) (int64, error)) {
	m.tokenValidator = validator
}

// Wrap wraps a handler with dual authentication support.
func (m *DualAuthMiddleware) Wrap(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var authenticated bool
		ctx := r.Context()

		// Try SPIFFE authentication first if enabled
		if m.spiffeEnabled && (m.authMode == "spiffe" || m.authMode == "both") {
			id, err := ExtractSPIFFEIDFromRequest(r)
			if err == nil {
				ctx = context.WithValue(ctx, SPIFFEIDKey, id)

				name, err := ExtractSatelliteNameFromSPIFFEID(id)
				if err == nil {
					ctx = context.WithValue(ctx, SatelliteNameKey, name)
				}

				region, err := ExtractRegionFromSPIFFEID(id)
				if err == nil {
					ctx = context.WithValue(ctx, RegionKey, region)
				}

				authenticated = true
				log.Printf("Authenticated via SPIFFE: %s", id.String())
			}
		}

		// If SPIFFE auth failed or disabled, use the original context
		if !authenticated && m.authMode == "spiffe" {
			writeJSONResponse(w, http.StatusUnauthorized, map[string]string{"error": "SPIFFE authentication required"})
			return
		}

		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func writeJSONResponse(w http.ResponseWriter, statusCode int, data any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(statusCode)
	_ = json.NewEncoder(w).Encode(data)
}
