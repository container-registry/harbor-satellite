//go:build nospiffe

package spiffe

import (
	"context"
	"encoding/json"
	"net/http"

	"github.com/spiffe/go-spiffe/v2/spiffeid"
)

// contextKey is a custom type for context keys to avoid collisions.
type contextKey string

const (
	SPIFFEIDKey      contextKey = "spiffe_id"
	SatelliteNameKey contextKey = "satellite_name"
	RegionKey        contextKey = "region"
)

// AuthMiddleware is a no-op when SPIFFE is disabled.
func AuthMiddleware(next http.Handler) http.Handler {
	return next
}

// RequireSPIFFEAuth returns 501 Not Implemented when SPIFFE is disabled.
func RequireSPIFFEAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusNotImplemented)
		if err := json.NewEncoder(w).Encode(map[string]string{"error": "SPIFFE not available in this build"}); err != nil {
			http.Error(w, "failed to encode response", http.StatusInternalServerError)
		}
	})
}

// GetSPIFFEID always returns false when SPIFFE is disabled.
func GetSPIFFEID(_ context.Context) (spiffeid.ID, bool) {
	return spiffeid.ID{}, false
}

// GetSatelliteName retrieves the satellite name from the request context.
func GetSatelliteName(ctx context.Context) (string, bool) {
	name, ok := ctx.Value(SatelliteNameKey).(string)
	return name, ok
}

// ContextWithSatelliteName returns a new context with the satellite name.
func ContextWithSatelliteName(ctx context.Context, name string) context.Context {
	return context.WithValue(ctx, SatelliteNameKey, name)
}

// GetRegion always returns false when SPIFFE is disabled.
func GetRegion(_ context.Context) (string, bool) {
	return "", false
}

// ExtractSatelliteNameFromSPIFFEID returns an error when SPIFFE is disabled.
func ExtractSatelliteNameFromSPIFFEID(_ spiffeid.ID) (string, error) {
	return "", ErrSPIFFENotAvailable
}

// NewSatelliteAuthorizer returns nil when SPIFFE is disabled.
func NewSatelliteAuthorizer(_ spiffeid.TrustDomain) *SatelliteAuthorizer {
	return nil
}

// SatelliteAuthorizer is a stub when SPIFFE is disabled.
type SatelliteAuthorizer struct{}

// AuthorizeID returns nil when SPIFFE is disabled.
func (a *SatelliteAuthorizer) AuthorizeID() func(spiffeid.ID) error {
	return nil
}
