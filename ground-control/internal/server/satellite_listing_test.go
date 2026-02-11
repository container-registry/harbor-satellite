package server

import (
	"database/sql"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/gorilla/mux"
	"github.com/lib/pq"
	"github.com/stretchr/testify/require"
)

func TestListSatelliteHandler(t *testing.T) {
	t.Run("returns satellites", func(t *testing.T) {
		server, mock := newMockServer(t)
		now := time.Now().UTC().Truncate(time.Second)

		rows := sqlmock.NewRows([]string{"id", "name", "created_at", "updated_at", "last_seen", "heartbeat_interval"}).
			AddRow(1, "edge-01", now, now, sql.NullTime{Time: now, Valid: true}, sql.NullString{String: "30s", Valid: true}).
			AddRow(2, "edge-02", now, now, sql.NullTime{}, sql.NullString{})
		mock.ExpectQuery("SELECT .+ FROM satellites").WillReturnRows(rows)

		req := httptest.NewRequest(http.MethodGet, "/api/satellites", nil)
		rr := httptest.NewRecorder()
		server.listSatelliteHandler(rr, req)

		require.Equal(t, http.StatusOK, rr.Code)
		require.Contains(t, rr.Body.String(), "edge-01")
		require.Contains(t, rr.Body.String(), "edge-02")
		require.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("empty list", func(t *testing.T) {
		server, mock := newMockServer(t)

		rows := sqlmock.NewRows([]string{"id", "name", "created_at", "updated_at", "last_seen", "heartbeat_interval"})
		mock.ExpectQuery("SELECT .+ FROM satellites").WillReturnRows(rows)

		req := httptest.NewRequest(http.MethodGet, "/api/satellites", nil)
		rr := httptest.NewRecorder()
		server.listSatelliteHandler(rr, req)

		require.Equal(t, http.StatusOK, rr.Code)
		require.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("db error returns 500", func(t *testing.T) {
		server, mock := newMockServer(t)

		mock.ExpectQuery("SELECT .+ FROM satellites").WillReturnError(fmt.Errorf("db error"))

		req := httptest.NewRequest(http.MethodGet, "/api/satellites", nil)
		rr := httptest.NewRecorder()
		server.listSatelliteHandler(rr, req)

		require.Equal(t, http.StatusInternalServerError, rr.Code)
		require.NoError(t, mock.ExpectationsWereMet())
	})
}

func TestGetActiveSatellitesHandler(t *testing.T) {
	t.Run("returns active satellites", func(t *testing.T) {
		server, mock := newMockServer(t)
		now := time.Now().UTC().Truncate(time.Second)

		rows := sqlmock.NewRows([]string{"id", "name", "created_at", "updated_at", "last_seen", "heartbeat_interval", "last_activity", "last_status_time"}).
			AddRow(1, "edge-01", now, now, sql.NullTime{Time: now, Valid: true}, sql.NullString{String: "30s", Valid: true}, "syncing", now)
		mock.ExpectQuery("SELECT .+ FROM satellites").WillReturnRows(rows)

		req := httptest.NewRequest(http.MethodGet, "/api/satellites/active", nil)
		rr := httptest.NewRecorder()
		server.getActiveSatellitesHandler(rr, req)

		require.Equal(t, http.StatusOK, rr.Code)
		require.Contains(t, rr.Body.String(), "edge-01")
		require.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("empty returns 200", func(t *testing.T) {
		server, mock := newMockServer(t)

		rows := sqlmock.NewRows([]string{"id", "name", "created_at", "updated_at", "last_seen", "heartbeat_interval", "last_activity", "last_status_time"})
		mock.ExpectQuery("SELECT .+ FROM satellites").WillReturnRows(rows)

		req := httptest.NewRequest(http.MethodGet, "/api/satellites/active", nil)
		rr := httptest.NewRecorder()
		server.getActiveSatellitesHandler(rr, req)

		require.Equal(t, http.StatusOK, rr.Code)
		require.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("db error returns 500", func(t *testing.T) {
		server, mock := newMockServer(t)

		mock.ExpectQuery("SELECT .+ FROM satellites").WillReturnError(fmt.Errorf("db error"))

		req := httptest.NewRequest(http.MethodGet, "/api/satellites/active", nil)
		rr := httptest.NewRecorder()
		server.getActiveSatellitesHandler(rr, req)

		require.Equal(t, http.StatusInternalServerError, rr.Code)
		require.NoError(t, mock.ExpectationsWereMet())
	})
}

func TestGetStaleSatellitesHandler(t *testing.T) {
	t.Run("returns stale satellites", func(t *testing.T) {
		server, mock := newMockServer(t)
		now := time.Now().UTC().Truncate(time.Second)

		rows := sqlmock.NewRows([]string{"id", "name", "created_at", "updated_at", "last_seen", "heartbeat_interval", "seconds_since_seen"}).
			AddRow(1, "stale-01", now, now, sql.NullTime{Time: now.Add(-time.Hour), Valid: true}, sql.NullString{String: "30s", Valid: true}, int64(3600))
		mock.ExpectQuery("SELECT .+ FROM satellites").WillReturnRows(rows)

		req := httptest.NewRequest(http.MethodGet, "/api/satellites/stale", nil)
		rr := httptest.NewRecorder()
		server.getStaleSatellitesHandler(rr, req)

		require.Equal(t, http.StatusOK, rr.Code)
		require.Contains(t, rr.Body.String(), "stale-01")
		require.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("db error returns 500", func(t *testing.T) {
		server, mock := newMockServer(t)

		mock.ExpectQuery("SELECT .+ FROM satellites").WillReturnError(fmt.Errorf("db error"))

		req := httptest.NewRequest(http.MethodGet, "/api/satellites/stale", nil)
		rr := httptest.NewRecorder()
		server.getStaleSatellitesHandler(rr, req)

		require.Equal(t, http.StatusInternalServerError, rr.Code)
		require.NoError(t, mock.ExpectationsWereMet())
	})
}

func TestGetSatelliteStatusHandler(t *testing.T) {
	t.Run("returns status", func(t *testing.T) {
		server, mock := newMockServer(t)
		now := time.Now().UTC().Truncate(time.Second)

		satRows := sqlmock.NewRows([]string{"id", "name", "created_at", "updated_at", "last_seen", "heartbeat_interval"}).
			AddRow(1, "edge-01", now, now, sql.NullTime{}, sql.NullString{})
		mock.ExpectQuery("SELECT .+ FROM satellites WHERE name").
			WithArgs("edge-01").
			WillReturnRows(satRows)

		statusRows := sqlmock.NewRows([]string{
			"id", "satellite_id", "activity", "latest_state_digest", "latest_config_digest",
			"cpu_percent", "memory_used_bytes", "storage_used_bytes", "last_sync_duration_ms",
			"image_count", "reported_at", "created_at", "artifact_ids",
		}).AddRow(
			1, 1, "syncing", sql.NullString{String: "sha256:abc", Valid: true}, sql.NullString{},
			sql.NullString{String: "12.50", Valid: true}, sql.NullInt64{Int64: 1024, Valid: true},
			sql.NullInt64{}, sql.NullInt64{},
			sql.NullInt32{Int32: 3, Valid: true}, now, now, pq.Array([]int32{1, 2, 3}),
		)
		mock.ExpectQuery("SELECT .+ FROM satellite_status").
			WithArgs(int32(1)).
			WillReturnRows(statusRows)

		req := httptest.NewRequest(http.MethodGet, "/api/satellites/edge-01/status", nil)
		req = mux.SetURLVars(req, map[string]string{"satellite": "edge-01"})

		rr := httptest.NewRecorder()
		server.getSatelliteStatusHandler(rr, req)

		require.Equal(t, http.StatusOK, rr.Code)
		require.Contains(t, rr.Body.String(), "syncing")
		require.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("satellite not found returns 404", func(t *testing.T) {
		server, mock := newMockServer(t)

		mock.ExpectQuery("SELECT .+ FROM satellites WHERE name").
			WithArgs("unknown").
			WillReturnError(sql.ErrNoRows)

		req := httptest.NewRequest(http.MethodGet, "/api/satellites/unknown/status", nil)
		req = mux.SetURLVars(req, map[string]string{"satellite": "unknown"})

		rr := httptest.NewRecorder()
		server.getSatelliteStatusHandler(rr, req)

		require.Equal(t, http.StatusNotFound, rr.Code)
		require.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("no status returns 404", func(t *testing.T) {
		server, mock := newMockServer(t)
		now := time.Now().UTC().Truncate(time.Second)

		satRows := sqlmock.NewRows([]string{"id", "name", "created_at", "updated_at", "last_seen", "heartbeat_interval"}).
			AddRow(1, "edge-01", now, now, sql.NullTime{}, sql.NullString{})
		mock.ExpectQuery("SELECT .+ FROM satellites WHERE name").
			WithArgs("edge-01").
			WillReturnRows(satRows)

		mock.ExpectQuery("SELECT .+ FROM satellite_status").
			WithArgs(int32(1)).
			WillReturnError(sql.ErrNoRows)

		req := httptest.NewRequest(http.MethodGet, "/api/satellites/edge-01/status", nil)
		req = mux.SetURLVars(req, map[string]string{"satellite": "edge-01"})

		rr := httptest.NewRecorder()
		server.getSatelliteStatusHandler(rr, req)

		require.Equal(t, http.StatusNotFound, rr.Code)
		require.NoError(t, mock.ExpectationsWereMet())
	})
}
