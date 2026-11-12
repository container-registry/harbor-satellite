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
	"github.com/container-registry/harbor-satellite/internal/shared/testutil/pending"
	"github.com/stretchr/testify/require"
)

var satelliteAuthPaths = []string{"/satellites/sync", "/satellites/refresh"}

func newSatelliteAuthServer(t *testing.T) (*Server, sqlmock.Sqlmock) {
	t.Helper()
	s, mock := newMockServer(t)
	s.rateLimiter = groundcontrolmiddleware.NewRateLimiter(10, time.Minute)
	t.Cleanup(s.rateLimiter.Stop)
	return s, mock
}

// withSVID makes req look like a mutual TLS request from satellite edge-1.
func withSVID(t *testing.T, req *http.Request) *http.Request {
	t.Helper()
	id, err := url.Parse("spiffe://harbor-satellite.local/satellite/region/default/edge-1")
	require.NoError(t, err)
	req.TLS = &tls.ConnectionState{PeerCertificates: []*x509.Certificate{{URIs: []*url.URL{id}}}}
	return req
}

// expectEdge1 lets the SVID's satellite edge-1 exist in the database.
func expectEdge1(mock sqlmock.Sqlmock) {
	now := time.Now().UTC()
	mock.ExpectQuery("SELECT .+ FROM satellites WHERE name").
		WithArgs("edge-1").
		WillReturnRows(sqlmock.NewRows([]string{"id", "name", "created_at", "updated_at", "last_seen", "heartbeat_interval"}).
			AddRow(1, "edge-1", now, now, sql.NullTime{}, sql.NullString{}))
}

// serveSatelliteAuthChain sends req through the route security middleware,
// which applies satelliteAuthChain to the satellite paths, and returns the
// status and the satellite name the handler behind it saw.
func serveSatelliteAuthChain(s *Server, req *http.Request) (int, string) {
	var satellite string
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		satellite, _ = spiffe.GetSatelliteName(r.Context())
		w.WriteHeader(http.StatusOK)
	})
	rec := httptest.NewRecorder()
	s.routeSecurityMiddleware(next).ServeHTTP(rec, req)
	return rec.Code, satellite
}

func TestSatelliteAuthChain_SVIDOnly(t *testing.T) {
	pending.Plan(t, "D3", "SatelliteAuthMiddleware runs before spiffe.AuthMiddleware, so a satellite with only an SVID gets 401")
	for _, path := range satelliteAuthPaths {
		t.Run(path, func(t *testing.T) {
			s, mock := newSatelliteAuthServer(t)
			expectEdge1(mock)

			status, satellite := serveSatelliteAuthChain(s, withSVID(t, httptest.NewRequest(http.MethodPost, path, nil)))

			require.Equal(t, http.StatusOK, status)
			require.Equal(t, "edge-1", satellite)
		})
	}
}

func TestSatelliteAuthChain_SVIDWinsOverInvalidRobot(t *testing.T) {
	pending.Plan(t, "D3", "with an SVID and robot Basic auth, the robot is checked first and its failure rejects the SVID")
	for _, path := range satelliteAuthPaths {
		t.Run(path, func(t *testing.T) {
			s, mock := newSatelliteAuthServer(t)
			expectEdge1(mock)
			req := withSVID(t, httptest.NewRequest(http.MethodPost, path, nil))
			req.SetBasicAuth("robot$nope", "x")

			status, satellite := serveSatelliteAuthChain(s, req)

			require.Equal(t, http.StatusOK, status)
			require.Equal(t, "edge-1", satellite)
		})
	}
}

func TestSatelliteAuthChain_NoCredentials(t *testing.T) {
	for _, path := range satelliteAuthPaths {
		t.Run(path, func(t *testing.T) {
			s, _ := newSatelliteAuthServer(t)

			status, _ := serveSatelliteAuthChain(s, httptest.NewRequest(http.MethodPost, path, nil))

			require.Equal(t, http.StatusUnauthorized, status)
		})
	}
}
