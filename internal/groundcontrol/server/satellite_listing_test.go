package server

import (
	"database/sql"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/container-registry/harbor-satellite/internal/groundcontrol/adapters/postgres"
	"github.com/container-registry/harbor-satellite/internal/groundcontrol/fleet"
	"github.com/container-registry/harbor-satellite/internal/groundcontrol/fleet/fleettest"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/gorilla/mux"
	"github.com/lib/pq"
	"github.com/stretchr/testify/require"
)

// viewColumns are the columns of ListSatelliteViewsByState.
var viewColumns = []string{
	"id", "name", "created_at", "updated_at", "last_seen", "heartbeat_interval", "state", "last_activity", "last_reported_at",
	"applied_state_digest", "applied_config_digest", "applied_states", "inventory_digest", "last_replication_error", "revoked_at",
	"view_updated_at", "desired_version", "rendered_digest", "published_digest", "published_artifact_digest", "publish_error",
	"published_at", "published_groups",
}

func viewRow(rows *sqlmock.Rows, id int32, name, state string, now, lastSeen time.Time) *sqlmock.Rows {
	return rows.AddRow(id, name, now, now, sql.NullTime{Time: lastSeen, Valid: true}, sql.NullString{String: "@every 00h00m30s", Valid: true},
		state, sql.NullString{String: "syncing", Valid: true}, sql.NullTime{Time: now, Valid: true}, nil, nil, []byte("[]"), nil, nil, nil,
		sql.NullTime{Time: now, Valid: true}, nil, nil, nil, nil, nil, nil, []byte("[]"))
}

// newMockViewServer is a mock server whose fleet services run on the mock
// database, as the view listings do.
func newMockViewServer(t *testing.T) (*Server, sqlmock.Sqlmock) {
	t.Helper()
	server, mock := newMockServer(t)
	repo, err := postgres.NewRepository(server.db)
	require.NoError(t, err)
	svc, err := fleet.NewServices(fleet.Deps{Repository: repo, Registry: fleettest.NewRegistry(nil)})
	require.NoError(t, err)
	server.services = svc
	return server, mock
}

func TestGetActiveSatellitesHandler(t *testing.T) {
	t.Run("returns online satellites from the view", func(t *testing.T) {
		server, mock := newMockViewServer(t)
		now := time.Now().UTC().Truncate(time.Second)
		mock.ExpectQuery("SELECT .+ FROM satellites s LEFT JOIN satellite_view").WithArgs("online").
			WillReturnRows(viewRow(sqlmock.NewRows(viewColumns), 1, "edge-01", "online", now, now))

		req := httptest.NewRequest(http.MethodGet, "/api/satellites/active", nil)
		rr := httptest.NewRecorder()
		server.ListActiveSatellites(rr, req)

		require.Equal(t, http.StatusOK, rr.Code)
		require.Contains(t, rr.Body.String(), "edge-01")
		require.Contains(t, rr.Body.String(), `"LastActivity":"syncing"`)
		require.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("empty returns 200", func(t *testing.T) {
		server, mock := newMockViewServer(t)
		mock.ExpectQuery("SELECT .+ FROM satellites s LEFT JOIN satellite_view").WithArgs("online").WillReturnRows(sqlmock.NewRows(viewColumns))

		req := httptest.NewRequest(http.MethodGet, "/api/satellites/active", nil)
		rr := httptest.NewRecorder()
		server.ListActiveSatellites(rr, req)

		require.Equal(t, http.StatusOK, rr.Code)
		require.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("db error returns 500", func(t *testing.T) {
		server, mock := newMockViewServer(t)
		mock.ExpectQuery("SELECT .+ FROM satellites s LEFT JOIN satellite_view").WillReturnError(fmt.Errorf("db error"))

		req := httptest.NewRequest(http.MethodGet, "/api/satellites/active", nil)
		rr := httptest.NewRecorder()
		server.ListActiveSatellites(rr, req)

		require.Equal(t, http.StatusInternalServerError, rr.Code)
		require.NoError(t, mock.ExpectationsWereMet())
	})
}

func TestGetStaleSatellitesHandler(t *testing.T) {
	t.Run("returns stale satellites from the view", func(t *testing.T) {
		server, mock := newMockViewServer(t)
		now := time.Now().UTC().Truncate(time.Second)
		mock.ExpectQuery("SELECT .+ FROM satellites s LEFT JOIN satellite_view").WithArgs("stale").
			WillReturnRows(viewRow(sqlmock.NewRows(viewColumns), 1, "stale-01", "stale", now, now.Add(-time.Hour)))

		req := httptest.NewRequest(http.MethodGet, "/api/satellites/stale", nil)
		rr := httptest.NewRecorder()
		server.ListStaleSatellites(rr, req)

		require.Equal(t, http.StatusOK, rr.Code)
		require.Contains(t, rr.Body.String(), "stale-01")
		require.Contains(t, rr.Body.String(), `"SecondsSinceSeen":3600`)
		require.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("db error returns 500", func(t *testing.T) {
		server, mock := newMockViewServer(t)
		mock.ExpectQuery("SELECT .+ FROM satellites s LEFT JOIN satellite_view").WillReturnError(fmt.Errorf("db error"))

		req := httptest.NewRequest(http.MethodGet, "/api/satellites/stale", nil)
		rr := httptest.NewRecorder()
		server.ListStaleSatellites(rr, req)

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
			"download_bytes_per_second", "bytes_in_window", "bytes_total",
		}).AddRow(
			1, 1, "syncing", sql.NullString{String: "sha256:abc", Valid: true}, sql.NullString{},
			sql.NullString{String: "12.50", Valid: true}, sql.NullInt64{Int64: 1024, Valid: true},
			sql.NullInt64{}, sql.NullInt64{},
			sql.NullInt32{Int32: 3, Valid: true}, now, now, pq.Array([]int32{1, 2, 3}),
			sql.NullFloat64{}, sql.NullInt64{}, sql.NullInt64{},
		)
		mock.ExpectQuery("SELECT .+ FROM satellite_status").
			WithArgs(int32(1)).
			WillReturnRows(statusRows)

		req := httptest.NewRequest(http.MethodGet, "/api/satellites/edge-01/status", nil)
		req = mux.SetURLVars(req, map[string]string{"satellite": "edge-01"})

		rr := httptest.NewRecorder()
		server.GetSatelliteStatus(rr, req, mux.Vars(req)["satellite"])

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
		server.GetSatelliteStatus(rr, req, mux.Vars(req)["satellite"])

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
		server.GetSatelliteStatus(rr, req, mux.Vars(req)["satellite"])

		require.Equal(t, http.StatusNotFound, rr.Code)
		require.NoError(t, mock.ExpectationsWereMet())
	})
}
