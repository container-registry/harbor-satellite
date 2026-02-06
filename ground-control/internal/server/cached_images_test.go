package server

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/container-registry/harbor-satellite/ground-control/internal/database"
	"github.com/gorilla/mux"
	"github.com/stretchr/testify/require"
)

func TestSyncHandler_WithCachedImages(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer db.Close()

	server := &Server{
		db:        db,
		dbQueries: database.New(db),
	}

	now := time.Now().UTC().Truncate(time.Second)

	// Mock GetSatelliteByName
	satRows := sqlmock.NewRows([]string{"id", "name", "created_at", "updated_at", "last_seen", "heartbeat_interval"}).
		AddRow(1, "edge-01", now, now, sql.NullTime{}, sql.NullString{})
	mock.ExpectQuery("SELECT .+ FROM satellites WHERE name").
		WithArgs("edge-01").
		WillReturnRows(satRows)

	// Mock InsertSatelliteStatus
	statusRows := sqlmock.NewRows([]string{
		"id", "satellite_id", "activity", "latest_state_digest", "latest_config_digest",
		"cpu_percent", "memory_used_bytes", "storage_used_bytes", "last_sync_duration_ms",
		"image_count", "reported_at", "created_at",
	}).AddRow(
		1, 1, "", sql.NullString{}, sql.NullString{},
		sql.NullString{}, sql.NullInt64{}, sql.NullInt64{}, sql.NullInt64{},
		sql.NullInt32{Int32: 2, Valid: true}, now, now,
	)
	mock.ExpectQuery("INSERT INTO satellite_status").WillReturnRows(statusRows)

	// Mock UpdateSatelliteLastSeen
	mock.ExpectExec("UPDATE satellites SET last_seen").WillReturnResult(sqlmock.NewResult(0, 1))

	// Mock InsertSatelliteCachedImage for each image
	mock.ExpectExec("INSERT INTO satellite_cached_images").
		WithArgs(int32(1), "localhost:8585/library/nginx:latest@sha256:abc", int64(50000), now).
		WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectExec("INSERT INTO satellite_cached_images").
		WithArgs(int32(1), "localhost:8585/library/alpine:3.18@sha256:def", int64(5000), now).
		WillReturnResult(sqlmock.NewResult(2, 1))

	reqBody := SatelliteStatusParams{
		Name:               "edge-01",
		ImageCount:         2,
		RequestCreatedTime: now,
		CachedImages: []CachedImage{
			{Reference: "localhost:8585/library/nginx:latest@sha256:abc", SizeBytes: 50000},
			{Reference: "localhost:8585/library/alpine:3.18@sha256:def", SizeBytes: 5000},
		},
	}
	body, _ := json.Marshal(reqBody)
	req := httptest.NewRequest(http.MethodPost, "/satellites/sync", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")

	rr := httptest.NewRecorder()
	server.syncHandler(rr, req)

	require.Equal(t, http.StatusOK, rr.Code)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestSyncHandler_NoCachedImages(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer db.Close()

	server := &Server{
		db:        db,
		dbQueries: database.New(db),
	}

	now := time.Now().UTC().Truncate(time.Second)

	satRows := sqlmock.NewRows([]string{"id", "name", "created_at", "updated_at", "last_seen", "heartbeat_interval"}).
		AddRow(1, "edge-01", now, now, sql.NullTime{}, sql.NullString{})
	mock.ExpectQuery("SELECT .+ FROM satellites WHERE name").
		WithArgs("edge-01").
		WillReturnRows(satRows)

	statusRows := sqlmock.NewRows([]string{
		"id", "satellite_id", "activity", "latest_state_digest", "latest_config_digest",
		"cpu_percent", "memory_used_bytes", "storage_used_bytes", "last_sync_duration_ms",
		"image_count", "reported_at", "created_at",
	}).AddRow(
		1, 1, "", sql.NullString{}, sql.NullString{},
		sql.NullString{}, sql.NullInt64{}, sql.NullInt64{}, sql.NullInt64{},
		sql.NullInt32{Int32: 0, Valid: true}, now, now,
	)
	mock.ExpectQuery("INSERT INTO satellite_status").WillReturnRows(statusRows)

	mock.ExpectExec("UPDATE satellites SET last_seen").WillReturnResult(sqlmock.NewResult(0, 1))

	// No InsertSatelliteCachedImage expected since CachedImages is empty

	reqBody := SatelliteStatusParams{
		Name:               "edge-01",
		RequestCreatedTime: now,
	}
	body, _ := json.Marshal(reqBody)
	req := httptest.NewRequest(http.MethodPost, "/satellites/sync", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")

	rr := httptest.NewRecorder()
	server.syncHandler(rr, req)

	require.Equal(t, http.StatusOK, rr.Code)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestSyncHandler_UnknownSatellite(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer db.Close()

	server := &Server{
		db:        db,
		dbQueries: database.New(db),
	}

	mock.ExpectQuery("SELECT .+ FROM satellites WHERE name").
		WithArgs("unknown").
		WillReturnError(sql.ErrNoRows)

	reqBody := SatelliteStatusParams{
		Name:               "unknown",
		RequestCreatedTime: time.Now().UTC(),
	}
	body, _ := json.Marshal(reqBody)
	req := httptest.NewRequest(http.MethodPost, "/satellites/sync", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")

	rr := httptest.NewRecorder()
	server.syncHandler(rr, req)

	require.Equal(t, http.StatusForbidden, rr.Code)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestGetCachedImagesHandler(t *testing.T) {
	t.Run("returns cached images for satellite", func(t *testing.T) {
		db, mock, err := sqlmock.New()
		require.NoError(t, err)
		defer db.Close()

		server := &Server{
			db:        db,
			dbQueries: database.New(db),
		}

		now := time.Now().UTC().Truncate(time.Second)

		satRows := sqlmock.NewRows([]string{"id", "name", "created_at", "updated_at", "last_seen", "heartbeat_interval"}).
			AddRow(1, "edge-01", now, now, sql.NullTime{}, sql.NullString{})
		mock.ExpectQuery("SELECT .+ FROM satellites WHERE name").
			WithArgs("edge-01").
			WillReturnRows(satRows)

		imageRows := sqlmock.NewRows([]string{"id", "satellite_id", "reference", "size_bytes", "reported_at", "created_at"}).
			AddRow(1, 1, "localhost:8585/library/nginx:latest@sha256:abc", int64(50000), now, now).
			AddRow(2, 1, "localhost:8585/library/alpine:3.18@sha256:def", int64(5000), now, now)
		mock.ExpectQuery("SELECT .+ FROM satellite_cached_images").
			WithArgs(int32(1)).
			WillReturnRows(imageRows)

		req := httptest.NewRequest(http.MethodGet, "/api/satellites/edge-01/images", nil)
		req = mux.SetURLVars(req, map[string]string{"satellite": "edge-01"})

		rr := httptest.NewRecorder()
		server.getCachedImagesHandler(rr, req)

		require.Equal(t, http.StatusOK, rr.Code)

		var images []database.SatelliteCachedImage
		err = json.NewDecoder(rr.Body).Decode(&images)
		require.NoError(t, err)
		require.Len(t, images, 2)
		require.Equal(t, "localhost:8585/library/nginx:latest@sha256:abc", images[0].Reference)
		require.Equal(t, int64(50000), images[0].SizeBytes)
		require.Equal(t, "localhost:8585/library/alpine:3.18@sha256:def", images[1].Reference)
		require.Equal(t, int64(5000), images[1].SizeBytes)

		require.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("satellite not found returns 404", func(t *testing.T) {
		db, mock, err := sqlmock.New()
		require.NoError(t, err)
		defer db.Close()

		server := &Server{
			db:        db,
			dbQueries: database.New(db),
		}

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
		db, mock, err := sqlmock.New()
		require.NoError(t, err)
		defer db.Close()

		server := &Server{
			db:        db,
			dbQueries: database.New(db),
		}

		now := time.Now().UTC().Truncate(time.Second)

		satRows := sqlmock.NewRows([]string{"id", "name", "created_at", "updated_at", "last_seen", "heartbeat_interval"}).
			AddRow(1, "edge-01", now, now, sql.NullTime{}, sql.NullString{})
		mock.ExpectQuery("SELECT .+ FROM satellites WHERE name").
			WithArgs("edge-01").
			WillReturnRows(satRows)

		emptyRows := sqlmock.NewRows([]string{"id", "satellite_id", "reference", "size_bytes", "reported_at", "created_at"})
		mock.ExpectQuery("SELECT .+ FROM satellite_cached_images").
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
