package server

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"regexp"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/gorilla/mux"
	"github.com/stretchr/testify/require"
)

func TestGetLabelsHandler(t *testing.T) {
	t.Run("returns labels for existing satellite", func(t *testing.T) {
		server, mock := newMockServer(t)
		now := time.Now().UTC().Truncate(time.Second)

		satRows := sqlmock.NewRows([]string{"id", "name", "created_at", "updated_at", "last_seen", "heartbeat_interval"}).
			AddRow(1, "edge-01", now, now, sql.NullTime{}, sql.NullString{})
		mock.ExpectQuery("SELECT .+ FROM satellites WHERE name").
			WithArgs("edge-01").
			WillReturnRows(satRows)

		labelRows := sqlmock.NewRows([]string{"key", "value"}).
			AddRow("env", "production").
			AddRow("region", "us-east")
		mock.ExpectQuery("SELECT key, value FROM satellite_labels").
			WithArgs(int32(1)).
			WillReturnRows(labelRows)

		req := httptest.NewRequest(http.MethodGet, "/api/satellites/edge-01/labels", nil)
		req = mux.SetURLVars(req, map[string]string{"satellite": "edge-01"})
		rr := httptest.NewRecorder()
		server.getLabelsHandler(rr, req)

		require.Equal(t, http.StatusOK, rr.Code)
		var got map[string]string
		require.NoError(t, json.Unmarshal(rr.Body.Bytes(), &got))
		require.Equal(t, "production", got["env"])
		require.Equal(t, "us-east", got["region"])
		require.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("returns 404 for unknown satellite", func(t *testing.T) {
		server, mock := newMockServer(t)

		mock.ExpectQuery("SELECT .+ FROM satellites WHERE name").
			WithArgs("ghost").
			WillReturnError(sql.ErrNoRows)

		req := httptest.NewRequest(http.MethodGet, "/api/satellites/ghost/labels", nil)
		req = mux.SetURLVars(req, map[string]string{"satellite": "ghost"})
		rr := httptest.NewRecorder()
		server.getLabelsHandler(rr, req)

		require.Equal(t, http.StatusNotFound, rr.Code)
		require.NoError(t, mock.ExpectationsWereMet())
	})
}

func TestSetLabelsHandler(t *testing.T) {
	t.Run("replaces all labels", func(t *testing.T) {
		server, mock := newMockServer(t)
		now := time.Now().UTC().Truncate(time.Second)

		satRows := sqlmock.NewRows([]string{"id", "name", "created_at", "updated_at", "last_seen", "heartbeat_interval"}).
			AddRow(1, "edge-01", now, now, sql.NullTime{}, sql.NullString{})
		mock.ExpectQuery("SELECT .+ FROM satellites WHERE name").
			WithArgs("edge-01").
			WillReturnRows(satRows)

		mock.ExpectBegin()
		mock.ExpectExec(regexp.QuoteMeta(`DELETE FROM satellite_labels WHERE satellite_id = $1`)).
			WithArgs(int32(1)).
			WillReturnResult(sqlmock.NewResult(0, 0))
		mock.ExpectExec(regexp.QuoteMeta(`INSERT INTO satellite_labels (satellite_id, key, value) VALUES ($1, $2, $3)`)).
			WithArgs(int32(1), "env", "staging").
			WillReturnResult(sqlmock.NewResult(1, 1))
		mock.ExpectCommit()

		body, _ := json.Marshal(map[string]string{"env": "staging"})
		req := httptest.NewRequest(http.MethodPut, "/api/satellites/edge-01/labels", bytes.NewReader(body))
		req = mux.SetURLVars(req, map[string]string{"satellite": "edge-01"})
		rr := httptest.NewRecorder()
		server.setLabelsHandler(rr, req)

		require.Equal(t, http.StatusOK, rr.Code)
		var got map[string]string
		require.NoError(t, json.Unmarshal(rr.Body.Bytes(), &got))
		require.Equal(t, "staging", got["env"])
		require.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("rejects invalid label key", func(t *testing.T) {
		server, mock := newMockServer(t)
		now := time.Now().UTC().Truncate(time.Second)

		satRows := sqlmock.NewRows([]string{"id", "name", "created_at", "updated_at", "last_seen", "heartbeat_interval"}).
			AddRow(1, "edge-01", now, now, sql.NullTime{}, sql.NullString{})
		mock.ExpectQuery("SELECT .+ FROM satellites WHERE name").
			WithArgs("edge-01").
			WillReturnRows(satRows)

		body, _ := json.Marshal(map[string]string{"bad key!": "val"})
		req := httptest.NewRequest(http.MethodPut, "/api/satellites/edge-01/labels", bytes.NewReader(body))
		req = mux.SetURLVars(req, map[string]string{"satellite": "edge-01"})
		rr := httptest.NewRecorder()
		server.setLabelsHandler(rr, req)

		require.Equal(t, http.StatusBadRequest, rr.Code)
		require.Contains(t, rr.Body.String(), "label key")
		require.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("rejects value exceeding 63 characters", func(t *testing.T) {
		server, mock := newMockServer(t)
		now := time.Now().UTC().Truncate(time.Second)

		satRows := sqlmock.NewRows([]string{"id", "name", "created_at", "updated_at", "last_seen", "heartbeat_interval"}).
			AddRow(1, "edge-01", now, now, sql.NullTime{}, sql.NullString{})
		mock.ExpectQuery("SELECT .+ FROM satellites WHERE name").
			WithArgs("edge-01").
			WillReturnRows(satRows)

		longVal := make([]byte, 64)
		for i := range longVal {
			longVal[i] = 'a'
		}
		body, _ := json.Marshal(map[string]string{"env": string(longVal)})
		req := httptest.NewRequest(http.MethodPut, "/api/satellites/edge-01/labels", bytes.NewReader(body))
		req = mux.SetURLVars(req, map[string]string{"satellite": "edge-01"})
		rr := httptest.NewRecorder()
		server.setLabelsHandler(rr, req)

		require.Equal(t, http.StatusBadRequest, rr.Code)
		require.Contains(t, rr.Body.String(), "63")
		require.NoError(t, mock.ExpectationsWereMet())
	})
}

func TestPatchLabelsHandler(t *testing.T) {
	t.Run("adds and removes labels", func(t *testing.T) {
		server, mock := newMockServer(t)
		now := time.Now().UTC().Truncate(time.Second)

		satRows := sqlmock.NewRows([]string{"id", "name", "created_at", "updated_at", "last_seen", "heartbeat_interval"}).
			AddRow(1, "edge-01", now, now, sql.NullTime{}, sql.NullString{})
		mock.ExpectQuery("SELECT .+ FROM satellites WHERE name").
			WithArgs("edge-01").
			WillReturnRows(satRows)

		mock.ExpectBegin()
		mock.ExpectExec(regexp.QuoteMeta(`DELETE FROM satellite_labels WHERE satellite_id = $1 AND key = $2`)).
			WithArgs(int32(1), "old-key").
			WillReturnResult(sqlmock.NewResult(0, 1))
		mock.ExpectCommit()

		labelRows := sqlmock.NewRows([]string{"key", "value"}).
			AddRow("env", "production")
		mock.ExpectQuery("SELECT key, value FROM satellite_labels").
			WithArgs(int32(1)).
			WillReturnRows(labelRows)

		val := (*string)(nil)
		body, _ := json.Marshal(map[string]*string{"old-key": val})
		req := httptest.NewRequest(http.MethodPatch, "/api/satellites/edge-01/labels", bytes.NewReader(body))
		req = mux.SetURLVars(req, map[string]string{"satellite": "edge-01"})
		rr := httptest.NewRecorder()
		server.patchLabelsHandler(rr, req)

		require.Equal(t, http.StatusOK, rr.Code)
		var got map[string]string
		require.NoError(t, json.Unmarshal(rr.Body.Bytes(), &got))
		require.Equal(t, "production", got["env"])
		require.NoError(t, mock.ExpectationsWereMet())
	})
}

func TestParseLabelSelectors(t *testing.T) {
	t.Run("parses equality selectors", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/satellites?label=env%3Dproduction&label=region%3Dus-east", nil)
		got, appErr := parseLabelSelectors(req.URL.Query())
		require.Nil(t, appErr)
		require.Len(t, got, 2)
		require.NotNil(t, got["env"])
		require.Equal(t, "production", *got["env"])
		require.NotNil(t, got["region"])
		require.Equal(t, "us-east", *got["region"])
	})

	t.Run("parses bare-key existence selector", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/satellites?label=env", nil)
		got, appErr := parseLabelSelectors(req.URL.Query())
		require.Nil(t, appErr)
		require.Len(t, got, 1)
		require.Nil(t, got["env"]) // nil means "key must exist"
	})

	t.Run("returns nil when no label params", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/satellites", nil)
		got, appErr := parseLabelSelectors(req.URL.Query())
		require.Nil(t, appErr)
		require.Nil(t, got)
	})

	t.Run("rejects empty key in equality selector", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/satellites?label=%3Dvalue", nil)
		_, appErr := parseLabelSelectors(req.URL.Query())
		require.NotNil(t, appErr)
		require.Equal(t, http.StatusBadRequest, appErr.Code)
		require.Contains(t, appErr.Message, "key must not be empty")
	})
}

func TestValidateLabelKey(t *testing.T) {
	valid := []string{"env", "app.io/name", "region-1", "a", "prefix/suffix"}
	for _, k := range valid {
		require.NoError(t, validateLabelKey(k), "expected valid: %s", k)
	}

	invalid := []string{"", "bad key", "key!", "has space"}
	for _, k := range invalid {
		require.Error(t, validateLabelKey(k), "expected invalid: %s", k)
	}
}
