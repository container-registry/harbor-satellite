package server

import (
	"database/sql"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/container-registry/harbor-satellite/ground-control/internal/spiffe"
	"github.com/container-registry/harbor-satellite/ground-control/pkg/crypto"
	"github.com/stretchr/testify/require"
)

func TestRequestIDMiddleware_GeneratesWhenAbsent(t *testing.T) {
	var s Server
	var seen string
	h := s.RequestIDMiddleware(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		seen = requestIDFromContext(r.Context())
	}))

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/login", nil))

	require.NotEmpty(t, seen, "a request ID should be generated when none is supplied")
	require.Equal(t, seen, rec.Header().Get("X-Request-ID"), "generated ID should be echoed on the response")
}

func TestRequestIDMiddleware_ReusesInboundHeader(t *testing.T) {
	var s Server
	var seen string
	h := s.RequestIDMiddleware(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		seen = requestIDFromContext(r.Context())
	}))

	req := httptest.NewRequest(http.MethodGet, "/login", nil)
	req.Header.Set("X-Request-ID", "abc-123")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	require.Equal(t, "abc-123", seen, "an inbound X-Request-ID should be reused")
	require.Equal(t, "abc-123", rec.Header().Get("X-Request-ID"))
}

func TestRequestIDMiddleware_RejectsMalformedHeader(t *testing.T) {
	var s Server
	var seen string
	h := s.RequestIDMiddleware(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		seen = requestIDFromContext(r.Context())
	}))

	for name, bad := range map[string]string{
		"too long":          strings.Repeat("a", maxRequestIDLen+1),
		"control chars":     "abc\ndef",
		"disallowed symbol": "id;rm -rf",
		"spaces":            "id with spaces",
	} {
		t.Run(name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/login", nil)
			req.Header.Set("X-Request-ID", bad)
			h.ServeHTTP(httptest.NewRecorder(), req)

			require.NotEqual(t, bad, seen, "malformed header must be rejected")
			require.True(t, validRequestID(seen), "fallback ID must itself be valid")
		})
	}
}

func TestRequestIDFromContext_DefaultsToEmpty(t *testing.T) {
	require.Empty(t, requestIDFromContext(httptest.NewRequest(http.MethodGet, "/", nil).Context()))
}

func TestSatelliteAuthMiddleware_NoCredentials(t *testing.T) {
	server, _ := newMockServer(t)

	h := server.SatelliteAuthMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/satellites/sync", nil))

	require.Equal(t, http.StatusUnauthorized, rec.Code)
	require.Contains(t, rec.Body.String(), "Unauthorized")
}

func TestSatelliteAuthMiddleware_ValidBasicAuth(t *testing.T) {
	server, mock := newMockServer(t)
	t.Cleanup(func() { require.NoError(t, mock.ExpectationsWereMet()) })

	hashed, err := crypto.HashSecret("robot-secret")
	require.NoError(t, err)

	// Mock GetRobotAccByRobotName
	robotRows := sqlmock.NewRows([]string{"id", "robot_name", "robot_secret_hash", "robot_id", "satellite_id", "robot_expiry", "created_at", "updated_at"}).
		AddRow(1, "robot$satellite-edge-01", hashed, "100", 10, nil, time.Now(), time.Now())
	mock.ExpectQuery("SELECT id, robot_name, robot_secret_hash, robot_id, satellite_id, robot_expiry, created_at, updated_at FROM robot_accounts WHERE robot_name = \\$1").
		WithArgs("robot$satellite-edge-01").
		WillReturnRows(robotRows)

	// Mock GetSatellite
	satRows := sqlmock.NewRows([]string{"id", "name", "created_at", "updated_at", "last_seen", "heartbeat_interval"}).
		AddRow(10, "edge-01", time.Now(), time.Now(), sql.NullTime{}, sql.NullString{})
	mock.ExpectQuery("SELECT id, name, created_at, updated_at, last_seen, heartbeat_interval FROM satellites WHERE id = \\$1").
		WithArgs(int32(10)).
		WillReturnRows(satRows)

	var nextCalled bool
	var seenName string
	h := server.SatelliteAuthMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		nextCalled = true
		if name, ok := spiffe.GetSatelliteName(r.Context()); ok {
			seenName = name
		}
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodPost, "/satellites/sync", nil)
	req.SetBasicAuth("robot$satellite-edge-01", "robot-secret")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	require.True(t, nextCalled)
	require.Equal(t, "edge-01", seenName)
	require.Equal(t, http.StatusOK, rec.Code)
}

func TestSatelliteAuthMiddleware_ValidSPIFFE(t *testing.T) {
	server, mock := newMockServer(t)
	t.Cleanup(func() { require.NoError(t, mock.ExpectationsWereMet()) })

	// Mock GetSatelliteByName
	satRows := sqlmock.NewRows([]string{"id", "name", "created_at", "updated_at", "last_seen", "heartbeat_interval"}).
		AddRow(10, "edge-01", time.Now(), time.Now(), sql.NullTime{}, sql.NullString{})
	mock.ExpectQuery("SELECT id, name, created_at, updated_at, last_seen, heartbeat_interval FROM satellites WHERE name = \\$1").
		WithArgs("edge-01").
		WillReturnRows(satRows)

	var nextCalled bool
	var seenName string
	h := server.SatelliteAuthMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		nextCalled = true
		if name, ok := spiffe.GetSatelliteName(r.Context()); ok {
			seenName = name
		}
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodPost, "/satellites/sync", nil)
	ctx := spiffe.ContextWithSatelliteName(req.Context(), "edge-01")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req.WithContext(ctx))

	require.True(t, nextCalled)
	require.Equal(t, "edge-01", seenName)
	require.Equal(t, http.StatusOK, rec.Code)
}

func TestSatelliteAuthMiddleware_UnknownRobotBasicAuth(t *testing.T) {
	server, mock := newMockServer(t)
	t.Cleanup(func() { require.NoError(t, mock.ExpectationsWereMet()) })

	mock.ExpectQuery("SELECT id, robot_name, robot_secret_hash, robot_id, satellite_id, robot_expiry, created_at, updated_at FROM robot_accounts WHERE robot_name = \\$1").
		WithArgs("robot$satellite-unknown").
		WillReturnError(sql.ErrNoRows)

	h := server.SatelliteAuthMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodPost, "/satellites/sync", nil)
	req.SetBasicAuth("robot$satellite-unknown", "any-password")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	require.Equal(t, http.StatusUnauthorized, rec.Code)
	require.Contains(t, rec.Body.String(), "Invalid credentials")
}

func TestSatelliteAuthMiddleware_InvalidBasicAuth(t *testing.T) {
	server, mock := newMockServer(t)
	t.Cleanup(func() { require.NoError(t, mock.ExpectationsWereMet()) })

	hashed, err := crypto.HashSecret("robot-secret")
	require.NoError(t, err)

	// Mock GetRobotAccByRobotName
	robotRows := sqlmock.NewRows([]string{"id", "robot_name", "robot_secret_hash", "robot_id", "satellite_id", "robot_expiry", "created_at", "updated_at"}).
		AddRow(1, "robot$satellite-edge-01", hashed, "100", 10, nil, time.Now(), time.Now())
	mock.ExpectQuery("SELECT id, robot_name, robot_secret_hash, robot_id, satellite_id, robot_expiry, created_at, updated_at FROM robot_accounts WHERE robot_name = \\$1").
		WithArgs("robot$satellite-edge-01").
		WillReturnRows(robotRows)

	h := server.SatelliteAuthMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodPost, "/satellites/sync", nil)
	req.SetBasicAuth("robot$satellite-edge-01", "wrong-secret")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	require.Equal(t, http.StatusUnauthorized, rec.Code)
	require.Contains(t, rec.Body.String(), "Invalid credentials")
}

func TestSatelliteAuthMiddleware_ExpiredBasicAuth(t *testing.T) {
	server, mock := newMockServer(t)
	t.Cleanup(func() { require.NoError(t, mock.ExpectationsWereMet()) })

	hashed, err := crypto.HashSecret("robot-secret")
	require.NoError(t, err)

	// Mock GetRobotAccByRobotName with expired time (1 hour ago)
	expiredTime := time.Now().Add(-1 * time.Hour)
	robotRows := sqlmock.NewRows([]string{"id", "robot_name", "robot_secret_hash", "robot_id", "satellite_id", "robot_expiry", "created_at", "updated_at"}).
		AddRow(1, "robot$satellite-edge-01", hashed, "100", 10, expiredTime, time.Now(), time.Now())
	mock.ExpectQuery("SELECT id, robot_name, robot_secret_hash, robot_id, satellite_id, robot_expiry, created_at, updated_at FROM robot_accounts WHERE robot_name = \\$1").
		WithArgs("robot$satellite-edge-01").
		WillReturnRows(robotRows)

	h := server.SatelliteAuthMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodPost, "/satellites/sync", nil)
	req.SetBasicAuth("robot$satellite-edge-01", "robot-secret")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	require.Equal(t, http.StatusUnauthorized, rec.Code)
	require.Contains(t, rec.Body.String(), "Unauthorized")
}
