package server

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/container-registry/harbor-satellite/ground-control/internal/database"
	"github.com/container-registry/harbor-satellite/ground-control/internal/models"
	configpkg "github.com/container-registry/harbor-satellite/pkg/config"
	jsonpatch "github.com/evanphx/json-patch"
	"github.com/gorilla/mux"
	"github.com/stretchr/testify/require"
)

func mustMarshalRequestConfig(t *testing.T, body []byte) json.RawMessage {
	t.Helper()

	var req models.ConfigObject
	require.NoError(t, json.Unmarshal(body, &req))

	configJSON, err := json.Marshal(req.Config)
	require.NoError(t, err)
	return configJSON
}

func mustMergePatchedConfig(t *testing.T, existing json.RawMessage, body []byte) json.RawMessage {
	t.Helper()

	var req configpkg.Config
	require.NoError(t, json.Unmarshal(body, &req))

	configJSON, err := json.Marshal(req)
	require.NoError(t, err)

	patchedJSON, err := jsonpatch.MergePatch(existing, configJSON)
	require.NoError(t, err)
	return patchedJSON
}

func TestListConfigsHandler(t *testing.T) {
	t.Run("returns config list", func(t *testing.T) {
		server, mock := newMockServer(t)
		now := time.Now().UTC().Truncate(time.Second)

		rows := sqlmock.NewRows([]string{"id", "config_name", "registry_url", "config", "created_at", "updated_at"}).
			AddRow(1, "test-config", "http://harbor:8080", json.RawMessage(`{"app_config":{}}`), now, now).
			AddRow(2, "prod-config", "http://harbor:8080", json.RawMessage(`{"app_config":{}}`), now, now)
		mock.ExpectQuery("SELECT .+ FROM configs").WillReturnRows(rows)

		req := httptest.NewRequest(http.MethodGet, "/api/configs", nil)
		rr := httptest.NewRecorder()
		server.listConfigsHandler(rr, req)

		require.Equal(t, http.StatusOK, rr.Code)
		require.Contains(t, rr.Body.String(), "test-config")
		require.Contains(t, rr.Body.String(), "prod-config")
		require.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("empty list", func(t *testing.T) {
		server, mock := newMockServer(t)

		rows := sqlmock.NewRows([]string{"id", "config_name", "registry_url", "config", "created_at", "updated_at"})
		mock.ExpectQuery("SELECT .+ FROM configs").WillReturnRows(rows)

		req := httptest.NewRequest(http.MethodGet, "/api/configs", nil)
		rr := httptest.NewRecorder()
		server.listConfigsHandler(rr, req)

		require.Equal(t, http.StatusOK, rr.Code)
		require.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("db error returns 500", func(t *testing.T) {
		server, mock := newMockServer(t)

		mock.ExpectQuery("SELECT .+ FROM configs").WillReturnError(fmt.Errorf("db error"))

		req := httptest.NewRequest(http.MethodGet, "/api/configs", nil)
		rr := httptest.NewRecorder()
		server.listConfigsHandler(rr, req)

		require.Equal(t, http.StatusInternalServerError, rr.Code)
		require.NoError(t, mock.ExpectationsWereMet())
	})
}

func TestGetConfigHandler(t *testing.T) {
	t.Run("found returns 200", func(t *testing.T) {
		server, mock := newMockServer(t)
		now := time.Now().UTC().Truncate(time.Second)

		rows := sqlmock.NewRows([]string{"id", "config_name", "registry_url", "config", "created_at", "updated_at"}).
			AddRow(1, "test-config", "http://harbor:8080", json.RawMessage(`{"app_config":{}}`), now, now)
		mock.ExpectQuery("SELECT .+ FROM configs WHERE config_name").
			WithArgs("test-config").
			WillReturnRows(rows)

		req := httptest.NewRequest(http.MethodGet, "/api/configs/test-config", nil)
		req = mux.SetURLVars(req, map[string]string{"config": "test-config"})

		rr := httptest.NewRecorder()
		server.getConfigHandler(rr, req)

		require.Equal(t, http.StatusOK, rr.Code)
		require.Contains(t, rr.Body.String(), "test-config")
		require.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("not found returns 404", func(t *testing.T) {
		server, mock := newMockServer(t)

		mock.ExpectQuery("SELECT .+ FROM configs WHERE config_name").
			WithArgs("nonexistent").
			WillReturnError(sql.ErrNoRows)

		req := httptest.NewRequest(http.MethodGet, "/api/configs/nonexistent", nil)
		req = mux.SetURLVars(req, map[string]string{"config": "nonexistent"})

		rr := httptest.NewRecorder()
		server.getConfigHandler(rr, req)

		require.Equal(t, http.StatusNotFound, rr.Code)
		require.NoError(t, mock.ExpectationsWereMet())
	})
}

func TestCreateConfigHandler_InvalidBody(t *testing.T) {
	server, _ := newMockServer(t)

	req := httptest.NewRequest(http.MethodPost, "/api/configs", bytes.NewReader([]byte("not json")))
	req.Header.Set("Content-Type", "application/json")

	rr := httptest.NewRecorder()
	server.createConfigHandler(rr, req)

	require.Equal(t, http.StatusBadRequest, rr.Code)
}

func TestCreateConfigHandler_EmptyName(t *testing.T) {
	server, _ := newMockServer(t)

	body, _ := json.Marshal(map[string]string{"config_name": ""})
	req := httptest.NewRequest(http.MethodPost, "/api/configs", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")

	rr := httptest.NewRecorder()
	server.createConfigHandler(rr, req)

	require.Equal(t, http.StatusBadRequest, rr.Code)
}

func TestCreateConfigHandler_PublishFailureDeletesCommittedConfig(t *testing.T) {
	server, mock := newMockServer(t)
	now := time.Now().UTC().Truncate(time.Second)
	body := []byte(`{"config_name":"test-config","config":{"app_config":{"log_level":"info"}}}`)
	configJSON := mustMarshalRequestConfig(t, body)

	server.EnsureSatelliteProjectExistsFn = func(context.Context) error { return nil }
	server.CreateAndPushConfigStateArtifactFn = func(context.Context, []byte, string) error { return fmt.Errorf("push failed") }

	mock.ExpectBegin()
	mock.ExpectQuery("INSERT INTO configs").
		WithArgs("test-config", "", configJSON).
		WillReturnRows(sqlmock.NewRows([]string{"id", "config_name", "registry_url", "config", "created_at", "updated_at"}).
			AddRow(1, "test-config", "", configJSON, now, now))
	mock.ExpectCommit()
	mock.ExpectExec("DELETE FROM configs").
		WithArgs(int32(1)).
		WillReturnResult(sqlmock.NewResult(0, 1))

	req := httptest.NewRequest(http.MethodPost, "/api/configs", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")

	rr := httptest.NewRecorder()
	server.createConfigHandler(rr, req)

	require.Equal(t, http.StatusBadGateway, rr.Code)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestCreateConfigHandler_PostCommitUsesDeadlineContext(t *testing.T) {
	server, mock := newMockServer(t)
	now := time.Now().UTC().Truncate(time.Second)
	body := []byte(`{"config_name":"test-config","config":{"app_config":{"log_level":"info"}}}`)
	configJSON := mustMarshalRequestConfig(t, body)

	server.EnsureSatelliteProjectExistsFn = func(ctx context.Context) error {
		_, ok := ctx.Deadline()
		require.True(t, ok)
		return fmt.Errorf("ensure failed")
	}
	server.CreateAndPushConfigStateArtifactFn = func(context.Context, []byte, string) error { return nil }

	mock.ExpectBegin()
	mock.ExpectQuery("INSERT INTO configs").
		WithArgs("test-config", "", configJSON).
		WillReturnRows(sqlmock.NewRows([]string{"id", "config_name", "registry_url", "config", "created_at", "updated_at"}).
			AddRow(1, "test-config", "", configJSON, now, now))
	mock.ExpectCommit()
	mock.ExpectExec("DELETE FROM configs").
		WithArgs(int32(1)).
		WillReturnResult(sqlmock.NewResult(0, 1))

	req := httptest.NewRequest(http.MethodPost, "/api/configs", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")

	rr := httptest.NewRecorder()
	server.createConfigHandler(rr, req)

	require.Equal(t, http.StatusInternalServerError, rr.Code)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestUpdateConfigHandler_PublishFailureRestoresCommittedConfigWhenUnchanged(t *testing.T) {
	server, mock := newMockServer(t)
	createdAt := time.Now().UTC().Add(-2 * time.Minute).Truncate(time.Second)
	originalUpdatedAt := createdAt.Add(30 * time.Second)
	savedUpdatedAt := createdAt.Add(60 * time.Second)
	restoredUpdatedAt := createdAt.Add(90 * time.Second)
	existingJSON := json.RawMessage(`{"app_config":{"log_level":"info"}}`)
	patchBody := []byte(`{"app_config":{"log_level":"debug"}}`)
	patchedJSON := mustMergePatchedConfig(t, existingJSON, patchBody)
	restoreResult := database.Config{
		ID:          1,
		ConfigName:  "test-config",
		RegistryUrl: "",
		Config:      existingJSON,
		CreatedAt:   createdAt,
		UpdatedAt:   restoredUpdatedAt,
	}

	server.EnsureSatelliteProjectExistsFn = func(ctx context.Context) error {
		_, ok := ctx.Deadline()
		require.True(t, ok)
		return nil
	}
	server.CreateAndPushConfigStateArtifactFn = func(ctx context.Context, data []byte, name string) error {
		_, ok := ctx.Deadline()
		require.True(t, ok)
		require.JSONEq(t, string(patchedJSON), string(data))
		require.Equal(t, "test-config", name)
		return fmt.Errorf("push failed")
	}

	mock.ExpectBegin()
	mock.ExpectQuery("SELECT .+ FROM configs WHERE config_name").
		WithArgs("test-config").
		WillReturnRows(sqlmock.NewRows([]string{"id", "config_name", "registry_url", "config", "created_at", "updated_at"}).
			AddRow(1, "test-config", "", existingJSON, createdAt, originalUpdatedAt))
	mock.ExpectQuery("UPDATE configs").
		WithArgs("test-config", "", patchedJSON).
		WillReturnRows(sqlmock.NewRows([]string{"id", "config_name", "registry_url", "config", "created_at", "updated_at"}).
			AddRow(1, "test-config", "", patchedJSON, createdAt, savedUpdatedAt))
	mock.ExpectCommit()
	mock.ExpectQuery("UPDATE configs").
		WithArgs("test-config", "", existingJSON, int32(1), createdAt, savedUpdatedAt).
		WillReturnRows(sqlmock.NewRows([]string{"id", "config_name", "registry_url", "config", "created_at", "updated_at"}).
			AddRow(restoreResult.ID, restoreResult.ConfigName, restoreResult.RegistryUrl, restoreResult.Config, restoreResult.CreatedAt, restoreResult.UpdatedAt))

	req := httptest.NewRequest(http.MethodPatch, "/api/configs/test-config", bytes.NewReader(patchBody))
	req = mux.SetURLVars(req, map[string]string{"config": "test-config"})
	req.Header.Set("Content-Type", "application/json")

	rr := httptest.NewRecorder()
	server.updateConfigHandler(rr, req)

	require.Equal(t, http.StatusBadGateway, rr.Code)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestUpdateConfigHandler_PublishFailureSkipsRestoreAfterConcurrentUpdate(t *testing.T) {
	server, mock := newMockServer(t)
	createdAt := time.Now().UTC().Add(-2 * time.Minute).Truncate(time.Second)
	originalUpdatedAt := createdAt.Add(30 * time.Second)
	savedUpdatedAt := createdAt.Add(60 * time.Second)
	existingJSON := json.RawMessage(`{"app_config":{"log_level":"info"}}`)
	patchBody := []byte(`{"app_config":{"log_level":"debug"}}`)
	patchedJSON := mustMergePatchedConfig(t, existingJSON, patchBody)

	server.EnsureSatelliteProjectExistsFn = func(context.Context) error { return nil }
	server.CreateAndPushConfigStateArtifactFn = func(context.Context, []byte, string) error { return fmt.Errorf("push failed") }

	mock.ExpectBegin()
	mock.ExpectQuery("SELECT .+ FROM configs WHERE config_name").
		WithArgs("test-config").
		WillReturnRows(sqlmock.NewRows([]string{"id", "config_name", "registry_url", "config", "created_at", "updated_at"}).
			AddRow(1, "test-config", "", existingJSON, createdAt, originalUpdatedAt))
	mock.ExpectQuery("UPDATE configs").
		WithArgs("test-config", "", patchedJSON).
		WillReturnRows(sqlmock.NewRows([]string{"id", "config_name", "registry_url", "config", "created_at", "updated_at"}).
			AddRow(1, "test-config", "", patchedJSON, createdAt, savedUpdatedAt))
	mock.ExpectCommit()
	mock.ExpectQuery("UPDATE configs").
		WithArgs("test-config", "", existingJSON, int32(1), createdAt, savedUpdatedAt).
		WillReturnError(sql.ErrNoRows)

	req := httptest.NewRequest(http.MethodPatch, "/api/configs/test-config", bytes.NewReader(patchBody))
	req = mux.SetURLVars(req, map[string]string{"config": "test-config"})
	req.Header.Set("Content-Type", "application/json")

	rr := httptest.NewRecorder()
	server.updateConfigHandler(rr, req)

	require.Equal(t, http.StatusConflict, rr.Code)
	require.Contains(t, rr.Body.String(), "rollback skipped")
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestDeleteConfigHandler_NotFound(t *testing.T) {
	server, mock := newMockServer(t)

	mock.ExpectQuery("SELECT .+ FROM configs WHERE config_name").
		WithArgs("nonexistent").
		WillReturnError(fmt.Errorf("db error"))

	req := httptest.NewRequest(http.MethodDelete, "/api/configs/nonexistent", nil)
	req = mux.SetURLVars(req, map[string]string{"config": "nonexistent"})

	rr := httptest.NewRecorder()
	server.deleteConfigHandler(rr, req)

	require.Equal(t, http.StatusInternalServerError, rr.Code)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestDeleteConfigHandler_ConfigInUse(t *testing.T) {
	server, mock := newMockServer(t)
	now := time.Now().UTC().Truncate(time.Second)

	mock.ExpectQuery("SELECT .+ FROM configs WHERE config_name").
		WithArgs("used-config").
		WillReturnRows(sqlmock.NewRows([]string{"id", "config_name", "registry_url", "config", "created_at", "updated_at"}).
			AddRow(1, "used-config", "http://harbor:8080", json.RawMessage(`{}`), now, now))

	mock.ExpectQuery("SELECT .+ FROM satellite_configs").
		WithArgs(int32(1)).
		WillReturnRows(sqlmock.NewRows([]string{"satellite_id", "config_id"}).
			AddRow(1, 1))

	req := httptest.NewRequest(http.MethodDelete, "/api/configs/used-config", nil)
	req = mux.SetURLVars(req, map[string]string{"config": "used-config"})

	rr := httptest.NewRecorder()
	server.deleteConfigHandler(rr, req)

	require.Equal(t, http.StatusBadRequest, rr.Code)
	require.NoError(t, mock.ExpectationsWereMet())
}
