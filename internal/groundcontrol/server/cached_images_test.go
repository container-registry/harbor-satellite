package server

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/container-registry/harbor-satellite/internal/groundcontrol/database"
	"github.com/gorilla/mux"
	"github.com/lib/pq"
	"github.com/stretchr/testify/require"
)

func newMockServer(t *testing.T) (*Server, sqlmock.Sqlmock) {
	t.Helper()

	db, mock, err := sqlmock.New()
	require.NoError(t, err)

	t.Cleanup(func() {
		mock.ExpectClose()
		require.NoError(t, db.Close())
	})

	return &Server{
		db:        db,
		dbQueries: database.New(db),
	}, mock
}

func mockSyncFlow(mock sqlmock.Sqlmock, name string, now time.Time, refs []string, sizes []int64) {
	satRows := sqlmock.NewRows([]string{"id", "name", "created_at", "updated_at", "last_seen", "heartbeat_interval"}).
		AddRow(1, name, now, now, sql.NullTime{}, sql.NullString{})
	mock.ExpectQuery("SELECT .+ FROM satellites WHERE name").
		WithArgs(name).
		WillReturnRows(satRows)

	if len(refs) > 0 {
		mock.ExpectExec("INSERT INTO artifacts").
			WithArgs(pq.Array(refs), pq.Array(sizes)).
			WillReturnResult(sqlmock.NewResult(0, int64(len(refs))))

		artifactRows := sqlmock.NewRows([]string{"id", "reference", "size_bytes", "created_at"})
		for i, ref := range refs {
			artifactRows.AddRow(int32(10+i), ref, sizes[i], now)
		}
		mock.ExpectQuery("SELECT .+ FROM artifacts").
			WithArgs(pq.Array(refs)).
			WillReturnRows(artifactRows)
	}

	var artifactIDs []int32
	if len(refs) > 0 {
		for i := range refs {
			artifactIDs = append(artifactIDs, int32(10+i))
		}
	}

	statusRows := sqlmock.NewRows([]string{
		"id", "satellite_id", "activity", "latest_state_digest", "latest_config_digest",
		"cpu_percent", "memory_used_bytes", "storage_used_bytes", "last_sync_duration_ms",
		"image_count", "reported_at", "created_at", "artifact_ids",
	}).AddRow(
		1, 1, "", sql.NullString{}, sql.NullString{},
		sql.NullString{}, sql.NullInt64{}, sql.NullInt64{}, sql.NullInt64{},
		sql.NullInt32{Int32: int32(len(refs)), Valid: true}, now, now, pq.Array(artifactIDs),
	)
	mock.ExpectQuery("INSERT INTO satellite_status").WillReturnRows(statusRows)

	mock.ExpectExec("UPDATE satellites SET last_seen").WillReturnResult(sqlmock.NewResult(0, 1))
}

func TestSyncHandler_WithCachedImages(t *testing.T) {
	server, mock := newMockServer(t)

	now := time.Now().UTC().Truncate(time.Second)

	refs := []string{"localhost:8585/library/nginx:latest@sha256:abc", "localhost:8585/library/alpine:3.18@sha256:def"}
	sizes := []int64{50000, 5000}

	mockSyncFlow(mock, "edge-01", now, refs, sizes)

	reqBody := SatelliteStatusParams{
		Name:               "edge-01",
		ImageCount:         2,
		RequestCreatedTime: now,
		CachedImages: []CachedImage{
			{Reference: refs[0], SizeBytes: sizes[0]},
			{Reference: refs[1], SizeBytes: sizes[1]},
		},
	}
	body := mustMarshalJSON(t, reqBody)
	req := httptest.NewRequest(http.MethodPost, "/satellites/sync", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")

	rr := httptest.NewRecorder()
	server.syncHandler(rr, req)

	require.Equal(t, http.StatusOK, rr.Code)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestSyncHandler_NoCachedImages(t *testing.T) {
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
		1, 1, "", sql.NullString{}, sql.NullString{},
		sql.NullString{}, sql.NullInt64{}, sql.NullInt64{}, sql.NullInt64{},
		sql.NullInt32{Int32: 0, Valid: true}, now, now, pq.Array([]int32(nil)),
	)
	mock.ExpectQuery("INSERT INTO satellite_status").WillReturnRows(statusRows)

	mock.ExpectExec("UPDATE satellites SET last_seen").WillReturnResult(sqlmock.NewResult(0, 1))

	reqBody := SatelliteStatusParams{
		Name:               "edge-01",
		RequestCreatedTime: now,
	}
	body := mustMarshalJSON(t, reqBody)
	req := httptest.NewRequest(http.MethodPost, "/satellites/sync", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")

	rr := httptest.NewRecorder()
	server.syncHandler(rr, req)

	require.Equal(t, http.StatusOK, rr.Code)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestSyncHandler_UnknownSatellite(t *testing.T) {
	server, mock := newMockServer(t)

	mock.ExpectQuery("SELECT .+ FROM satellites WHERE name").
		WithArgs("unknown").
		WillReturnError(sql.ErrNoRows)

	reqBody := SatelliteStatusParams{
		Name:               "unknown",
		RequestCreatedTime: time.Now().UTC(),
	}
	body := mustMarshalJSON(t, reqBody)
	req := httptest.NewRequest(http.MethodPost, "/satellites/sync", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")

	rr := httptest.NewRecorder()
	server.syncHandler(rr, req)

	require.Equal(t, http.StatusForbidden, rr.Code)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestGetCachedImagesHandler(t *testing.T) {
	t.Run("returns cached images for satellite", func(t *testing.T) {
		server, mock := newMockServer(t)

		now := time.Now().UTC().Truncate(time.Second)

		satRows := sqlmock.NewRows([]string{"id", "name", "created_at", "updated_at", "last_seen", "heartbeat_interval"}).
			AddRow(1, "edge-01", now, now, sql.NullTime{}, sql.NullString{})
		mock.ExpectQuery("SELECT .+ FROM satellites WHERE name").
			WithArgs("edge-01").
			WillReturnRows(satRows)

		artifactRows := sqlmock.NewRows([]string{"id", "reference", "size_bytes", "created_at"}).
			AddRow(int32(10), "localhost:8585/library/nginx:latest@sha256:abc", int64(50000), now).
			AddRow(int32(11), "localhost:8585/library/alpine:3.18@sha256:def", int64(5000), now)
		mock.ExpectQuery("SELECT .+ FROM artifacts").
			WithArgs(int32(1)).
			WillReturnRows(artifactRows)

		req := httptest.NewRequest(http.MethodGet, "/api/satellites/edge-01/images", nil)
		req = mux.SetURLVars(req, map[string]string{"satellite": "edge-01"})

		rr := httptest.NewRecorder()
		server.getCachedImagesHandler(rr, req)

		require.Equal(t, http.StatusOK, rr.Code)

		var artifacts []struct {
			ID        int32     `json:"ID"`
			Reference string    `json:"Reference"`
			SizeBytes int64     `json:"SizeBytes"`
			CreatedAt time.Time `json:"CreatedAt"`
		}
		err := json.NewDecoder(rr.Body).Decode(&artifacts)
		require.NoError(t, err)
		require.Len(t, artifacts, 2)
		require.Equal(t, "localhost:8585/library/nginx:latest@sha256:abc", artifacts[0].Reference)
		require.Equal(t, int64(50000), artifacts[0].SizeBytes)
		require.Equal(t, "localhost:8585/library/alpine:3.18@sha256:def", artifacts[1].Reference)
		require.Equal(t, int64(5000), artifacts[1].SizeBytes)

		require.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("satellite not found returns 404", func(t *testing.T) {
		server, mock := newMockServer(t)

		mock.ExpectQuery("SELECT .+ FROM satellites WHERE name").
			WithArgs("nonexistent").
			WillReturnError(sql.ErrNoRows)

		req := httptest.NewRequest(http.MethodGet, "/api/satellites/nonexistent/images", nil)
		req = mux.SetURLVars(req, map[string]string{"satellite": "nonexistent"})

		rr := httptest.NewRecorder()
		server.getCachedImagesHandler(rr, req)

		require.Equal(t, http.StatusNotFound, rr.Code)
		require.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("no cached images returns empty array", func(t *testing.T) {
		server, mock := newMockServer(t)

		now := time.Now().UTC().Truncate(time.Second)

		satRows := sqlmock.NewRows([]string{"id", "name", "created_at", "updated_at", "last_seen", "heartbeat_interval"}).
			AddRow(1, "edge-01", now, now, sql.NullTime{}, sql.NullString{})
		mock.ExpectQuery("SELECT .+ FROM satellites WHERE name").
			WithArgs("edge-01").
			WillReturnRows(satRows)

		emptyRows := sqlmock.NewRows([]string{"id", "reference", "size_bytes", "created_at"})
		mock.ExpectQuery("SELECT .+ FROM artifacts").
			WithArgs(int32(1)).
			WillReturnRows(emptyRows)

		req := httptest.NewRequest(http.MethodGet, "/api/satellites/edge-01/images", nil)
		req = mux.SetURLVars(req, map[string]string{"satellite": "edge-01"})

		rr := httptest.NewRecorder()
		server.getCachedImagesHandler(rr, req)

		require.Equal(t, http.StatusOK, rr.Code)
		require.NoError(t, mock.ExpectationsWereMet())
	})
}

func TestSyncHandler_InvalidBody(t *testing.T) {
	server, _ := newMockServer(t)

	req := httptest.NewRequest(http.MethodPost, "/satellites/sync", bytes.NewReader([]byte("not json")))
	req.Header.Set("Content-Type", "application/json")

	rr := httptest.NewRecorder()
	server.syncHandler(rr, req)

	require.Equal(t, http.StatusBadRequest, rr.Code)
}

func TestSyncHandler_InvalidHeartbeatInterval(t *testing.T) {
	server, mock := newMockServer(t)

	now := time.Now().UTC().Truncate(time.Second)

	satRows := sqlmock.NewRows([]string{"id", "name", "created_at", "updated_at", "last_seen", "heartbeat_interval"}).
		AddRow(1, "edge-01", now, now, sql.NullTime{}, sql.NullString{})
	mock.ExpectQuery("SELECT .+ FROM satellites WHERE name").
		WithArgs("edge-01").
		WillReturnRows(satRows)

	reqBody := SatelliteStatusParams{
		Name:                "edge-01",
		StateReportInterval: "bad-format",
		RequestCreatedTime:  now,
	}
	body := mustMarshalJSON(t, reqBody)
	req := httptest.NewRequest(http.MethodPost, "/satellites/sync", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")

	rr := httptest.NewRecorder()
	server.syncHandler(rr, req)

	require.Equal(t, http.StatusBadRequest, rr.Code)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestSyncHandler_BatchInsertArtifactsFails(t *testing.T) {
	server, mock := newMockServer(t)

	now := time.Now().UTC().Truncate(time.Second)

	satRows := sqlmock.NewRows([]string{"id", "name", "created_at", "updated_at", "last_seen", "heartbeat_interval"}).
		AddRow(1, "edge-01", now, now, sql.NullTime{}, sql.NullString{})
	mock.ExpectQuery("SELECT .+ FROM satellites WHERE name").
		WithArgs("edge-01").
		WillReturnRows(satRows)

	mock.ExpectExec("INSERT INTO artifacts").
		WithArgs(
			pq.Array([]string{"localhost:8585/nginx:latest@sha256:abc"}),
			pq.Array([]int64{50000}),
		).
		WillReturnError(fmt.Errorf("db connection lost"))

	reqBody := SatelliteStatusParams{
		Name:               "edge-01",
		RequestCreatedTime: now,
		CachedImages: []CachedImage{
			{Reference: "localhost:8585/nginx:latest@sha256:abc", SizeBytes: 50000},
		},
	}
	body := mustMarshalJSON(t, reqBody)
	req := httptest.NewRequest(http.MethodPost, "/satellites/sync", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")

	rr := httptest.NewRecorder()
	server.syncHandler(rr, req)

	require.Equal(t, http.StatusInternalServerError, rr.Code)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestGetCachedImagesHandler_DBFailure(t *testing.T) {
	server, mock := newMockServer(t)

	now := time.Now().UTC().Truncate(time.Second)

	satRows := sqlmock.NewRows([]string{"id", "name", "created_at", "updated_at", "last_seen", "heartbeat_interval"}).
		AddRow(1, "edge-01", now, now, sql.NullTime{}, sql.NullString{})
	mock.ExpectQuery("SELECT .+ FROM satellites WHERE name").
		WithArgs("edge-01").
		WillReturnRows(satRows)

	mock.ExpectQuery("SELECT .+ FROM artifacts").
		WithArgs(int32(1)).
		WillReturnError(fmt.Errorf("db timeout"))

	req := httptest.NewRequest(http.MethodGet, "/api/satellites/edge-01/images", nil)
	req = mux.SetURLVars(req, map[string]string{"satellite": "edge-01"})

	rr := httptest.NewRecorder()
	server.getCachedImagesHandler(rr, req)

	require.Equal(t, http.StatusInternalServerError, rr.Code)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestCachedImageJSON(t *testing.T) {
	t.Run("serialization roundtrip", func(t *testing.T) {
		original := SatelliteStatusParams{
			Name:       "edge-01",
			ImageCount: 2,
			CachedImages: []CachedImage{
				{Reference: "localhost:8585/nginx:latest@sha256:abc", SizeBytes: 50000},
				{Reference: "localhost:8585/alpine:3.18@sha256:def", SizeBytes: 5000},
			},
		}

		data, err := json.Marshal(original)
		require.NoError(t, err)

		var decoded SatelliteStatusParams
		err = json.Unmarshal(data, &decoded)
		require.NoError(t, err)

		require.Equal(t, original.Name, decoded.Name)
		require.Equal(t, original.ImageCount, decoded.ImageCount)
		require.Len(t, decoded.CachedImages, 2)
		require.Equal(t, original.CachedImages[0].Reference, decoded.CachedImages[0].Reference)
		require.Equal(t, original.CachedImages[0].SizeBytes, decoded.CachedImages[0].SizeBytes)
		require.Equal(t, original.CachedImages[1].Reference, decoded.CachedImages[1].Reference)
		require.Equal(t, original.CachedImages[1].SizeBytes, decoded.CachedImages[1].SizeBytes)
	})

	t.Run("omits cached_images when empty", func(t *testing.T) {
		original := SatelliteStatusParams{
			Name:       "edge-01",
			ImageCount: 0,
		}

		data, err := json.Marshal(original)
		require.NoError(t, err)
		require.NotContains(t, string(data), "cached_images")
	})
}

func TestSyncHandler_OversizedCachedImages(t *testing.T) {
	server, mock := newMockServer(t)

	now := time.Now().UTC().Truncate(time.Second)

	// Create a payload with > 1000 items
	oversized := make([]CachedImage, 1001)
	for i := 0; i < 1001; i++ {
		oversized[i] = CachedImage{Reference: fmt.Sprintf("image-%d", i), SizeBytes: 100}
	}

	reqBody := SatelliteStatusParams{
		Name:               "edge-oversized",
		RequestCreatedTime: now,
		CachedImages:       oversized,
	}

	body := mustMarshalJSON(t, reqBody)
	req := httptest.NewRequest(http.MethodPost, "/satellites/sync", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")

	rr := httptest.NewRecorder()
	server.syncHandler(rr, req)

	require.Equal(t, http.StatusRequestEntityTooLarge, rr.Code)
	
	require.Equal(t, http.StatusRequestEntityTooLarge, rr.Code)
	
	// Expect that NO DB queries were executed because it was rejected early
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestSyncHandler_ExactlyMaxCachedImages(t *testing.T) {
	server, mock := newMockServer(t)

	now := time.Now().UTC().Truncate(time.Second)

	// Create a payload with exactly maxCachedImages (1000) items
	exactMax := make([]CachedImage, 1000)
	refs := make([]string, 1000)
	sizes := make([]int64, 1000)
	for i := 0; i < 1000; i++ {
		exactMax[i] = CachedImage{Reference: fmt.Sprintf("image-%d", i), SizeBytes: 100}
		refs[i] = exactMax[i].Reference
		sizes[i] = exactMax[i].SizeBytes
	}

	mockSyncFlow(mock, "edge-boundary", now, refs, sizes)

	reqBody := SatelliteStatusParams{
		Name:               "edge-boundary",
		RequestCreatedTime: now,
		CachedImages:       exactMax,
	}

	body := mustMarshalJSON(t, reqBody)
	req := httptest.NewRequest(http.MethodPost, "/satellites/sync", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")

	rr := httptest.NewRecorder()
	server.syncHandler(rr, req)

	require.Equal(t, http.StatusOK, rr.Code)
	require.NoError(t, mock.ExpectationsWereMet())
}
