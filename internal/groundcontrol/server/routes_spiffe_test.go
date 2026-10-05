//go:build !nospiffe

package server

import (
	"crypto/tls"
	"crypto/x509"
	"database/sql"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	groundcontrolmiddleware "github.com/container-registry/harbor-satellite/internal/groundcontrol/middleware"
	"github.com/container-registry/harbor-satellite/internal/groundcontrol/spiffe"
	"github.com/stretchr/testify/require"
)

// TestRouteSecurityMiddleware_SyncAcceptsSPIFFEOnly drives the wired
// /satellites/sync middleware chain with only a SPIFFE peer certificate and
// no robot credentials. The SPIFFE identity must be extracted before the
// satellite auth middleware decides on authorization.
func TestRouteSecurityMiddleware_SyncAcceptsSPIFFEOnly(t *testing.T) {
	server, mock := newMockServer(t)
	server.rateLimiter = groundcontrolmiddleware.NewRateLimiter(10, time.Minute)
	t.Cleanup(func() { require.NoError(t, mock.ExpectationsWereMet()) })

	satRows := sqlmock.NewRows([]string{"id", "name", "created_at", "updated_at", "last_seen", "heartbeat_interval"}).
		AddRow(10, "edge-01", time.Now(), time.Now(), sql.NullTime{}, sql.NullString{})
	mock.ExpectQuery("SELECT id, name, created_at, updated_at, last_seen, heartbeat_interval FROM satellites WHERE name = \\$1").
		WithArgs("edge-01").
		WillReturnRows(satRows)

	var reachedHandler bool
	var seenName string
	handler := server.routeSecurityMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		reachedHandler = true
		seenName, _ = spiffe.GetSatelliteName(r.Context())
		w.WriteHeader(http.StatusOK)
	}))

	spiffeURI, err := url.Parse("spiffe://example.org/satellite/edge-01")
	require.NoError(t, err)

	request := httptest.NewRequest(http.MethodPost, "/satellites/sync", nil)
	request.TLS = &tls.ConnectionState{
		PeerCertificates: []*x509.Certificate{{URIs: []*url.URL{spiffeURI}}},
	}
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)

	require.Equal(t, http.StatusOK, response.Code, response.Body.String())
	require.True(t, reachedHandler)
	require.Equal(t, "edge-01", seenName)
}
