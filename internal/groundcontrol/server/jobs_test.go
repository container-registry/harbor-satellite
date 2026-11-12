package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/stretchr/testify/require"
)

func healthOf(t *testing.T, s *Server) (int, HealthResponse) {
	t.Helper()
	rec := httptest.NewRecorder()
	s.Health(rec, httptest.NewRequest(http.MethodGet, "/health", nil))
	var resp HealthResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp), rec.Body.String())
	return rec.Code, resp
}

func TestHealth_StoppedJobIsUnhealthy(t *testing.T) {
	db, mock, err := sqlmock.New(sqlmock.MonitorPingsOption(true))
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	s := &Server{db: db, harborContact: contactFunc(func() time.Time { return time.Time{} }), harborStaleAfter: time.Minute}

	mock.ExpectPing()
	code, resp := healthOf(t, s)
	require.Equal(t, http.StatusOK, code)
	require.Equal(t, Degraded, resp.Status)

	s.JobStopped("async worker")
	s.JobStopped("async worker")
	mock.ExpectPing()
	code, resp = healthOf(t, s)
	require.Equal(t, http.StatusServiceUnavailable, code)
	require.Equal(t, Unhealthy, resp.Status, "a stopped job outranks a degraded Harbor")
	require.Equal(t, ComponentHealth{Status: "unhealthy", Detail: "stopped"}, resp.Components["async worker"])
	require.Equal(t, "degraded", resp.Components["harbor"].Status)
	require.Equal(t, []string{"async worker"}, s.StoppedJobs())
	require.NoError(t, mock.ExpectationsWereMet())
}
