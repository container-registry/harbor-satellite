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
	"github.com/gorilla/mux"
	"github.com/stretchr/testify/require"
)

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
