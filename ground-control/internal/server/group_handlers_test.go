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

func TestListGroupHandler(t *testing.T) {
	t.Run("returns groups", func(t *testing.T) {
		server, mock := newMockServer(t)
		now := time.Now().UTC().Truncate(time.Second)

		rows := sqlmock.NewRows([]string{"id", "group_name", "registry_url", "projects", "created_at", "updated_at"}).
			AddRow(1, "edge-group", "http://harbor:8080", pq.Array([]string{"edge"}), now, now).
			AddRow(2, "prod-group", "http://harbor:8080", pq.Array([]string{"prod", "staging"}), now, now)
		mock.ExpectQuery("SELECT .+ FROM groups").WillReturnRows(rows)

		req := httptest.NewRequest(http.MethodGet, "/api/groups", nil)
		rr := httptest.NewRecorder()
		server.listGroupHandler(rr, req)

		require.Equal(t, http.StatusOK, rr.Code)
		require.Contains(t, rr.Body.String(), "edge-group")
		require.Contains(t, rr.Body.String(), "prod-group")
		require.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("empty list", func(t *testing.T) {
		server, mock := newMockServer(t)

		rows := sqlmock.NewRows([]string{"id", "group_name", "registry_url", "projects", "created_at", "updated_at"})
		mock.ExpectQuery("SELECT .+ FROM groups").WillReturnRows(rows)

		req := httptest.NewRequest(http.MethodGet, "/api/groups", nil)
		rr := httptest.NewRecorder()
		server.listGroupHandler(rr, req)

		require.Equal(t, http.StatusOK, rr.Code)
		require.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("db error returns 500", func(t *testing.T) {
		server, mock := newMockServer(t)

		mock.ExpectQuery("SELECT .+ FROM groups").WillReturnError(fmt.Errorf("db error"))

		req := httptest.NewRequest(http.MethodGet, "/api/groups", nil)
		rr := httptest.NewRecorder()
		server.listGroupHandler(rr, req)

		require.Equal(t, http.StatusInternalServerError, rr.Code)
		require.NoError(t, mock.ExpectationsWereMet())
	})
}

func TestGetGroupHandler(t *testing.T) {
	t.Run("found returns 200", func(t *testing.T) {
		server, mock := newMockServer(t)
		now := time.Now().UTC().Truncate(time.Second)

		rows := sqlmock.NewRows([]string{"id", "group_name", "registry_url", "projects", "created_at", "updated_at"}).
			AddRow(1, "edge-group", "http://harbor:8080", pq.Array([]string{"edge"}), now, now)
		mock.ExpectQuery("SELECT .+ FROM groups WHERE group_name").
			WithArgs("edge-group").
			WillReturnRows(rows)

		req := httptest.NewRequest(http.MethodGet, "/api/groups/edge-group", nil)
		req = mux.SetURLVars(req, map[string]string{"group": "edge-group"})

		rr := httptest.NewRecorder()
		server.getGroupHandler(rr, req)

		require.Equal(t, http.StatusOK, rr.Code)
		require.Contains(t, rr.Body.String(), "edge-group")
		require.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("not found returns 404", func(t *testing.T) {
		server, mock := newMockServer(t)

		mock.ExpectQuery("SELECT .+ FROM groups WHERE group_name").
			WithArgs("nonexistent").
			WillReturnError(sql.ErrNoRows)

		req := httptest.NewRequest(http.MethodGet, "/api/groups/nonexistent", nil)
		req = mux.SetURLVars(req, map[string]string{"group": "nonexistent"})

		rr := httptest.NewRecorder()
		server.getGroupHandler(rr, req)

		require.Equal(t, http.StatusNotFound, rr.Code)
		require.NoError(t, mock.ExpectationsWereMet())
	})
}

func TestGroupSatelliteHandler(t *testing.T) {
	t.Run("group with satellites", func(t *testing.T) {
		server, mock := newMockServer(t)
		now := time.Now().UTC().Truncate(time.Second)

		mock.ExpectQuery("SELECT EXISTS").
			WithArgs("edge-group").
			WillReturnRows(sqlmock.NewRows([]string{"exists"}).AddRow(true))

		satRows := sqlmock.NewRows([]string{"id", "name", "created_at", "updated_at"}).
			AddRow(1, "sat-01", now, now).
			AddRow(2, "sat-02", now, now)
		mock.ExpectQuery("SELECT .+ FROM satellites").
			WithArgs("edge-group").
			WillReturnRows(satRows)

		req := httptest.NewRequest(http.MethodGet, "/api/groups/edge-group/satellites", nil)
		req = mux.SetURLVars(req, map[string]string{"group": "edge-group"})

		rr := httptest.NewRecorder()
		server.groupSatelliteHandler(rr, req)

		require.Equal(t, http.StatusOK, rr.Code)
		require.Contains(t, rr.Body.String(), "sat-01")
		require.Contains(t, rr.Body.String(), "sat-02")
		require.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("group not found returns 404", func(t *testing.T) {
		server, mock := newMockServer(t)

		mock.ExpectQuery("SELECT EXISTS").
			WithArgs("nonexistent").
			WillReturnRows(sqlmock.NewRows([]string{"exists"}).AddRow(false))

		req := httptest.NewRequest(http.MethodGet, "/api/groups/nonexistent/satellites", nil)
		req = mux.SetURLVars(req, map[string]string{"group": "nonexistent"})

		rr := httptest.NewRecorder()
		server.groupSatelliteHandler(rr, req)

		require.Equal(t, http.StatusNotFound, rr.Code)
		require.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("group exists no satellites returns empty array", func(t *testing.T) {
		server, mock := newMockServer(t)

		mock.ExpectQuery("SELECT EXISTS").
			WithArgs("empty-group").
			WillReturnRows(sqlmock.NewRows([]string{"exists"}).AddRow(true))

		mock.ExpectQuery("SELECT .+ FROM satellites").
			WithArgs("empty-group").
			WillReturnRows(sqlmock.NewRows([]string{"id", "name", "created_at", "updated_at"}))

		req := httptest.NewRequest(http.MethodGet, "/api/groups/empty-group/satellites", nil)
		req = mux.SetURLVars(req, map[string]string{"group": "empty-group"})

		rr := httptest.NewRecorder()
		server.groupSatelliteHandler(rr, req)

		require.Equal(t, http.StatusOK, rr.Code)
		require.Equal(t, "[]", rr.Body.String()[:2])
		require.NoError(t, mock.ExpectationsWereMet())
	})
}
