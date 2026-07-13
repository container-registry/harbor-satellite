package handlers

import (
	"database/sql"
	"net/http"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/container-registry/harbor-satellite/internal/env"
	swaggermodels "github.com/container-registry/harbor-satellite/internal/groundcontrol/swagger/models"
	"github.com/container-registry/harbor-satellite/internal/groundcontrol/swagger/server/operations/satellites"
	"github.com/go-openapi/strfmt"
	"github.com/lib/pq"
	"github.com/stretchr/testify/require"
)

func TestSpiffeRegistrationEnabled(t *testing.T) {
	original := env.GC
	t.Cleanup(func() { env.GC = original })

	env.GC = env.GroundControl{}
	require.False(t, spiffeRegistrationEnabled())

	env.GC.SPIFFE.Enabled = true
	require.True(t, spiffeRegistrationEnabled())

	env.GC = env.GroundControl{}
	env.GC.EmbeddedSPIRE.Enabled = true
	require.True(t, spiffeRegistrationEnabled())

	env.GC = env.GroundControl{}
	env.GC.SPIRE.ServerSocket = "/run/spire/server.sock"
	require.True(t, spiffeRegistrationEnabled())
}

func TestListSatellites(t *testing.T) {
	t.Run("returns satellites", func(t *testing.T) {
		mock := newMockHandlerService(t)
		now := time.Now().UTC().Truncate(time.Second)
		rows := sqlmock.NewRows([]string{"id", "name", "created_at", "updated_at", "last_seen", "heartbeat_interval"}).
			AddRow(1, "edge-01", now, now, sql.NullTime{Time: now, Valid: true}, sql.NullString{String: "@every 00h00m30s", Valid: true}).
			AddRow(2, "edge-02", now, now, sql.NullTime{}, sql.NullString{})
		mock.ExpectQuery("SELECT id, name, created_at, updated_at, last_seen, heartbeat_interval FROM satellites").WillReturnRows(rows)

		responder := ListSatellites(satellites.ListSatellitesParams{
			HTTPRequest: handlerRequest(http.MethodGet, "/api/satellites"),
		}, handlerTestPrincipal)

		response, ok := responder.(*satellites.ListSatellitesOK)
		require.True(t, ok)
		require.Len(t, response.Payload, 2)
		require.Equal(t, "edge-01", response.Payload[0].Name)
		require.True(t, response.Payload[0].LastSeen.Valid)
		require.False(t, response.Payload[1].LastSeen.Valid)
	})

	t.Run("returns an empty array", func(t *testing.T) {
		mock := newMockHandlerService(t)
		mock.ExpectQuery("SELECT id, name, created_at, updated_at, last_seen, heartbeat_interval FROM satellites").
			WillReturnRows(sqlmock.NewRows([]string{"id", "name", "created_at", "updated_at", "last_seen", "heartbeat_interval"}))

		responder := ListSatellites(satellites.ListSatellitesParams{
			HTTPRequest: handlerRequest(http.MethodGet, "/api/satellites"),
		}, handlerTestPrincipal)

		response, ok := responder.(*satellites.ListSatellitesOK)
		require.True(t, ok)
		require.NotNil(t, response.Payload)
		require.Empty(t, response.Payload)
	})
}

func TestGetCachedImages(t *testing.T) {
	t.Run("returns the latest artifacts", func(t *testing.T) {
		mock := newMockHandlerService(t)
		now := time.Now().UTC().Truncate(time.Second)
		mock.ExpectQuery("SELECT id, name, created_at, updated_at, last_seen, heartbeat_interval FROM satellites").
			WithArgs("edge-01").
			WillReturnRows(sqlmock.NewRows([]string{"id", "name", "created_at", "updated_at", "last_seen", "heartbeat_interval"}).
				AddRow(1, "edge-01", now, now, sql.NullTime{}, sql.NullString{}))
		mock.ExpectQuery("SELECT a.id, a.reference, a.size_bytes, a.created_at").
			WithArgs(int32(1)).
			WillReturnRows(sqlmock.NewRows([]string{"id", "reference", "size_bytes", "created_at"}).
				AddRow(10, "registry.example/library/nginx@sha256:abc", 50000, now).
				AddRow(11, "registry.example/library/alpine@sha256:def", 5000, now))

		responder := GetCachedImages(satellites.GetCachedImagesParams{
			HTTPRequest: handlerRequest(http.MethodGet, "/api/satellites/edge-01/images"),
			Satellite:   "edge-01",
		}, handlerTestPrincipal)

		response, ok := responder.(*satellites.GetCachedImagesOK)
		require.True(t, ok)
		require.Len(t, response.Payload, 2)
		require.Equal(t, int64(50000), response.Payload[0].SizeBytes)
	})

	t.Run("returns not found for an unknown satellite", func(t *testing.T) {
		mock := newMockHandlerService(t)
		mock.ExpectQuery("SELECT id, name, created_at, updated_at, last_seen, heartbeat_interval FROM satellites").
			WithArgs("missing").
			WillReturnError(sql.ErrNoRows)

		responder := GetCachedImages(satellites.GetCachedImagesParams{
			HTTPRequest: handlerRequest(http.MethodGet, "/api/satellites/missing/images"),
			Satellite:   "missing",
		}, handlerTestPrincipal)

		_, ok := responder.(*satellites.GetCachedImagesNotFound)
		require.True(t, ok)
	})
}

func TestGetSatelliteStatusWithoutReport(t *testing.T) {
	mock := newMockHandlerService(t)
	now := time.Now().UTC().Truncate(time.Second)
	mock.ExpectQuery("SELECT id, name, created_at, updated_at, last_seen, heartbeat_interval FROM satellites").
		WithArgs("edge-01").
		WillReturnRows(sqlmock.NewRows([]string{"id", "name", "created_at", "updated_at", "last_seen", "heartbeat_interval"}).
			AddRow(1, "edge-01", now, now, sql.NullTime{}, sql.NullString{}))
	mock.ExpectQuery("SELECT id, satellite_id, activity, latest_state_digest").
		WithArgs(int32(1)).
		WillReturnError(sql.ErrNoRows)

	responder := GetSatelliteStatus(satellites.GetSatelliteStatusParams{
		HTTPRequest: handlerRequest(http.MethodGet, "/api/satellites/edge-01/status"),
		Satellite:   "edge-01",
	}, handlerTestPrincipal)

	response, ok := responder.(*satellites.GetSatelliteStatusNotFound)
	require.True(t, ok)
	require.Equal(t, "no status available", response.Payload.Message)
}

func TestGetSatellite(t *testing.T) {
	t.Run("returns a matching satellite", func(t *testing.T) {
		mock := newMockHandlerService(t)
		now := time.Now().UTC().Truncate(time.Second)
		mock.ExpectQuery("SELECT id, name, created_at, updated_at, last_seen, heartbeat_interval FROM satellites").
			WithArgs("edge-01").
			WillReturnRows(sqlmock.NewRows([]string{"id", "name", "created_at", "updated_at", "last_seen", "heartbeat_interval"}).
				AddRow(1, "edge-01", now, now, sql.NullTime{Time: now, Valid: true}, sql.NullString{String: "@every 00h00m30s", Valid: true}))

		responder := GetSatellite(satellites.GetSatelliteParams{
			HTTPRequest: handlerRequest(http.MethodGet, "/api/satellites/edge-01"),
			Satellite:   "edge-01",
		}, handlerTestPrincipal)

		response, ok := responder.(*satellites.GetSatelliteOK)
		require.True(t, ok)
		require.Equal(t, "edge-01", response.Payload.Name)
		require.True(t, response.Payload.LastSeen.Valid)
	})

	t.Run("reports lookup failures", func(t *testing.T) {
		mock := newMockHandlerService(t)
		mock.ExpectQuery("SELECT id, name, created_at, updated_at, last_seen, heartbeat_interval FROM satellites").
			WithArgs("missing").
			WillReturnError(sql.ErrNoRows)

		responder := GetSatellite(satellites.GetSatelliteParams{
			HTTPRequest: handlerRequest(http.MethodGet, "/api/satellites/missing"),
			Satellite:   "missing",
		}, handlerTestPrincipal)

		_, ok := responder.(*satellites.GetSatelliteInternalServerError)
		require.True(t, ok)
	})
}

func TestListActiveSatellites(t *testing.T) {
	mock := newMockHandlerService(t)
	now := time.Now().UTC().Truncate(time.Second)
	mock.ExpectQuery("SELECT s.id, s.name, s.created_at, s.updated_at, s.last_seen, s.heartbeat_interval").
		WillReturnRows(sqlmock.NewRows([]string{
			"id", "name", "created_at", "updated_at", "last_seen", "heartbeat_interval", "last_activity", "last_status_time",
		}).AddRow(
			1, "edge-01", now, now, sql.NullTime{Time: now, Valid: true},
			sql.NullString{String: "@every 00h00m30s", Valid: true}, "syncing", now,
		))

	responder := ListActiveSatellites(satellites.ListActiveSatellitesParams{
		HTTPRequest: handlerRequest(http.MethodGet, "/api/satellites/active"),
	}, handlerTestPrincipal)

	response, ok := responder.(*satellites.ListActiveSatellitesOK)
	require.True(t, ok)
	require.Len(t, response.Payload, 1)
	require.Equal(t, "syncing", response.Payload[0].LastActivity)
}

func TestListStaleSatellites(t *testing.T) {
	mock := newMockHandlerService(t)
	now := time.Now().UTC().Truncate(time.Second)
	mock.ExpectQuery("SELECT s.id, s.name, s.created_at, s.updated_at, s.last_seen, s.heartbeat_interval").
		WillReturnRows(sqlmock.NewRows([]string{
			"id", "name", "created_at", "updated_at", "last_seen", "heartbeat_interval", "seconds_since_seen",
		}).AddRow(
			1, "edge-01", now, now, sql.NullTime{Time: now.Add(-time.Hour), Valid: true},
			sql.NullString{String: "@every 00h00m30s", Valid: true}, int64(3600),
		))

	responder := ListStaleSatellites(satellites.ListStaleSatellitesParams{
		HTTPRequest: handlerRequest(http.MethodGet, "/api/satellites/stale"),
	}, handlerTestPrincipal)

	response, ok := responder.(*satellites.ListStaleSatellitesOK)
	require.True(t, ok)
	require.Len(t, response.Payload, 1)
	require.Equal(t, int64(3600), response.Payload[0].SecondsSinceSeen)
}

func TestGetSatelliteStatus(t *testing.T) {
	mock := newMockHandlerService(t)
	now := time.Now().UTC().Truncate(time.Second)
	mock.ExpectQuery("SELECT id, name, created_at, updated_at, last_seen, heartbeat_interval FROM satellites").
		WithArgs("edge-01").
		WillReturnRows(sqlmock.NewRows([]string{"id", "name", "created_at", "updated_at", "last_seen", "heartbeat_interval"}).
			AddRow(1, "edge-01", now, now, sql.NullTime{}, sql.NullString{}))
	mock.ExpectQuery("SELECT id, satellite_id, activity, latest_state_digest").
		WithArgs(int32(1)).
		WillReturnRows(sqlmock.NewRows([]string{
			"id", "satellite_id", "activity", "latest_state_digest", "latest_config_digest", "cpu_percent",
			"memory_used_bytes", "storage_used_bytes", "last_sync_duration_ms", "image_count", "reported_at", "created_at", "artifact_ids",
		}).AddRow(
			4, 1, "syncing", sql.NullString{String: "sha256:state", Valid: true}, sql.NullString{},
			sql.NullString{String: "12.50", Valid: true}, sql.NullInt64{Int64: 1024, Valid: true},
			sql.NullInt64{Int64: 2048, Valid: true}, sql.NullInt64{Int64: 250, Valid: true},
			sql.NullInt32{Int32: 3, Valid: true}, now, now, pq.Array([]int32{10, 11}),
		))

	responder := GetSatelliteStatus(satellites.GetSatelliteStatusParams{
		HTTPRequest: handlerRequest(http.MethodGet, "/api/satellites/edge-01/status"),
		Satellite:   "edge-01",
	}, handlerTestPrincipal)

	response, ok := responder.(*satellites.GetSatelliteStatusOK)
	require.True(t, ok)
	require.Equal(t, "syncing", response.Payload.Activity)
	require.Equal(t, int64(1024), response.Payload.MemoryUsedBytes.Int64)
	require.Equal(t, []int32{10, 11}, response.Payload.ArtifactIds)
}

func TestSyncSatellite(t *testing.T) {
	t.Run("rejects an unknown satellite", func(t *testing.T) {
		mock := newMockHandlerService(t)
		mock.ExpectQuery("SELECT id, name, created_at, updated_at, last_seen, heartbeat_interval FROM satellites").
			WithArgs("missing").
			WillReturnError(sql.ErrNoRows)

		responder := SyncSatellite(satellites.SyncSatelliteParams{
			HTTPRequest: handlerRequest(http.MethodPost, "/satellites/sync"),
			Body:        &swaggermodels.SatelliteStatusParams{Name: "missing"},
		})

		_, ok := responder.(*satellites.SyncSatelliteForbidden)
		require.True(t, ok)
	})

	t.Run("rejects an invalid heartbeat interval", func(t *testing.T) {
		mock := newMockHandlerService(t)
		now := time.Now().UTC().Truncate(time.Second)
		mock.ExpectQuery("SELECT id, name, created_at, updated_at, last_seen, heartbeat_interval FROM satellites").
			WithArgs("edge-01").
			WillReturnRows(sqlmock.NewRows([]string{"id", "name", "created_at", "updated_at", "last_seen", "heartbeat_interval"}).
				AddRow(1, "edge-01", now, now, sql.NullTime{}, sql.NullString{}))

		responder := SyncSatellite(satellites.SyncSatelliteParams{
			HTTPRequest: handlerRequest(http.MethodPost, "/satellites/sync"),
			Body: &swaggermodels.SatelliteStatusParams{
				Name:                "edge-01",
				StateReportInterval: "invalid",
			},
		})

		_, ok := responder.(*satellites.SyncSatelliteBadRequest)
		require.True(t, ok)
	})

	t.Run("stores a report without cached images", func(t *testing.T) {
		mock := newMockHandlerService(t)
		now := time.Now().UTC().Truncate(time.Second)
		mock.ExpectQuery("SELECT id, name, created_at, updated_at, last_seen, heartbeat_interval FROM satellites").
			WithArgs("edge-01").
			WillReturnRows(sqlmock.NewRows([]string{"id", "name", "created_at", "updated_at", "last_seen", "heartbeat_interval"}).
				AddRow(1, "edge-01", now, now, sql.NullTime{}, sql.NullString{}))
		mock.ExpectQuery("INSERT INTO satellite_status").
			WillReturnRows(sqlmock.NewRows([]string{
				"id", "satellite_id", "activity", "latest_state_digest", "latest_config_digest", "cpu_percent",
				"memory_used_bytes", "storage_used_bytes", "last_sync_duration_ms", "image_count", "reported_at", "created_at", "artifact_ids",
			}).AddRow(
				4, 1, "idle", sql.NullString{}, sql.NullString{}, sql.NullString{String: "0.00", Valid: true},
				sql.NullInt64{Int64: 0, Valid: true}, sql.NullInt64{Int64: 0, Valid: true},
				sql.NullInt64{Int64: 0, Valid: true}, sql.NullInt32{Int32: 0, Valid: true}, now, now, pq.Array([]int32(nil)),
			))
		mock.ExpectExec("UPDATE satellites SET last_seen").WillReturnResult(sqlmock.NewResult(0, 1))

		responder := SyncSatellite(satellites.SyncSatelliteParams{
			HTTPRequest: handlerRequest(http.MethodPost, "/satellites/sync"),
			Body: &swaggermodels.SatelliteStatusParams{
				Name:               "edge-01",
				Activity:           "idle",
				RequestCreatedTime: strfmt.DateTime(now),
			},
		})

		_, ok := responder.(*satellites.SyncSatelliteOK)
		require.True(t, ok)
	})
}

func TestSatelliteHandlersRejectMissingBodies(t *testing.T) {
	original := env.GC
	t.Cleanup(func() { env.GC = original })
	env.GC = env.GroundControl{}
	newMockHandlerService(t)

	_, registerBadRequest := RegisterSatellite(satellites.RegisterSatelliteParams{
		HTTPRequest: handlerRequest(http.MethodPost, "/api/satellites"),
	}, handlerTestPrincipal).(*satellites.RegisterSatelliteBadRequest)
	require.True(t, registerBadRequest)

	_, syncBadRequest := SyncSatellite(satellites.SyncSatelliteParams{
		HTTPRequest: handlerRequest(http.MethodPost, "/satellites/sync"),
	}).(*satellites.SyncSatelliteBadRequest)
	require.True(t, syncBadRequest)
}

func TestNormalizeHeartbeatInterval(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		input   string
		want    string
		wantErr string
	}{
		{name: "empty", input: "", want: ""},
		{name: "seconds", input: "@every 30s", want: "@every 00h00m30s"},
		{name: "mixed duration", input: "@every 1h2m3s", want: "@every 01h02m03s"},
		{name: "missing prefix", input: "30s", wantErr: "must start"},
		{name: "subsecond", input: "@every 500ms", wantErr: "at least 1 second"},
		{name: "fractional second", input: "@every 1500ms", wantErr: "whole number of seconds"},
		{name: "non-positive", input: "@every 0s", wantErr: "positive"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			got, err := normalizeHeartbeatInterval(test.input)
			if test.wantErr != "" {
				require.ErrorContains(t, err, test.wantErr)
				return
			}
			require.NoError(t, err)
			require.Equal(t, test.want, got)
		})
	}
}
