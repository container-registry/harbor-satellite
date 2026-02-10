package server

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/container-registry/harbor-satellite/ground-control/internal/auth"
	"github.com/container-registry/harbor-satellite/ground-control/internal/database"
	"github.com/gorilla/mux"
	"github.com/lib/pq"
	"github.com/stretchr/testify/require"
)

func newMockServerWithAuth(t *testing.T) (*Server, sqlmock.Sqlmock) {
	t.Helper()

	db, mock, err := sqlmock.New()
	require.NoError(t, err)

	t.Cleanup(func() {
		mock.ExpectClose()
		require.NoError(t, db.Close())
	})

	return &Server{
		db:              db,
		dbQueries:       database.New(db),
		passwordPolicy:  auth.DefaultPolicy(),
		sessionDuration: time.Hour,
		lockoutDuration: 15 * time.Minute,
	}, mock
}

func TestCreateUserHandler(t *testing.T) {
	t.Run("success returns 201", func(t *testing.T) {
		server, mock := newMockServerWithAuth(t)
		now := time.Now().UTC().Truncate(time.Second)

		mock.ExpectQuery("INSERT INTO users").
			WithArgs("testuser", sqlmock.AnyArg(), "admin").
			WillReturnRows(sqlmock.NewRows([]string{"id", "username", "password_hash", "role", "created_at", "updated_at"}).
				AddRow(1, "testuser", "hashed", "admin", now, now))

		body, _ := json.Marshal(createUserRequest{Username: "testuser", Password: "SecurePass1"})
		req := httptest.NewRequest(http.MethodPost, "/api/users", bytes.NewReader(body))

		rr := httptest.NewRecorder()
		server.createUserHandler(rr, req)

		require.Equal(t, http.StatusCreated, rr.Code)
		require.Contains(t, rr.Body.String(), "testuser")
		require.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("empty username returns 400", func(t *testing.T) {
		server, _ := newMockServerWithAuth(t)

		body, _ := json.Marshal(createUserRequest{Username: "", Password: "SecurePass1"})
		req := httptest.NewRequest(http.MethodPost, "/api/users", bytes.NewReader(body))

		rr := httptest.NewRecorder()
		server.createUserHandler(rr, req)

		require.Equal(t, http.StatusBadRequest, rr.Code)
	})

	t.Run("reserved admin returns 400", func(t *testing.T) {
		server, _ := newMockServerWithAuth(t)

		body, _ := json.Marshal(createUserRequest{Username: "admin", Password: "SecurePass1"})
		req := httptest.NewRequest(http.MethodPost, "/api/users", bytes.NewReader(body))

		rr := httptest.NewRecorder()
		server.createUserHandler(rr, req)

		require.Equal(t, http.StatusBadRequest, rr.Code)
	})

	t.Run("duplicate returns 409", func(t *testing.T) {
		server, mock := newMockServerWithAuth(t)

		mock.ExpectQuery("INSERT INTO users").
			WithArgs("existing", sqlmock.AnyArg(), "admin").
			WillReturnError(&pq.Error{Code: "23505"})

		body, _ := json.Marshal(createUserRequest{Username: "existing", Password: "SecurePass1"})
		req := httptest.NewRequest(http.MethodPost, "/api/users", bytes.NewReader(body))

		rr := httptest.NewRecorder()
		server.createUserHandler(rr, req)

		require.Equal(t, http.StatusConflict, rr.Code)
		require.NoError(t, mock.ExpectationsWereMet())
	})
}

func TestListUsersHandler(t *testing.T) {
	t.Run("returns users", func(t *testing.T) {
		server, mock := newMockServerWithAuth(t)
		now := time.Now().UTC().Truncate(time.Second)

		rows := sqlmock.NewRows([]string{"id", "username", "role", "created_at", "updated_at"}).
			AddRow(1, "alice", "admin", now, now).
			AddRow(2, "bob", "admin", now, now)
		mock.ExpectQuery("SELECT .+ FROM users").WillReturnRows(rows)

		req := httptest.NewRequest(http.MethodGet, "/api/users", nil)
		rr := httptest.NewRecorder()
		server.listUsersHandler(rr, req)

		require.Equal(t, http.StatusOK, rr.Code)
		require.Contains(t, rr.Body.String(), "alice")
		require.Contains(t, rr.Body.String(), "bob")
		require.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("empty list", func(t *testing.T) {
		server, mock := newMockServerWithAuth(t)

		rows := sqlmock.NewRows([]string{"id", "username", "role", "created_at", "updated_at"})
		mock.ExpectQuery("SELECT .+ FROM users").WillReturnRows(rows)

		req := httptest.NewRequest(http.MethodGet, "/api/users", nil)
		rr := httptest.NewRecorder()
		server.listUsersHandler(rr, req)

		require.Equal(t, http.StatusOK, rr.Code)
		require.NoError(t, mock.ExpectationsWereMet())
	})
}

func TestGetUserHandler(t *testing.T) {
	t.Run("found returns 200", func(t *testing.T) {
		server, mock := newMockServerWithAuth(t)
		now := time.Now().UTC().Truncate(time.Second)

		rows := sqlmock.NewRows([]string{"id", "username", "password_hash", "role", "created_at", "updated_at"}).
			AddRow(1, "alice", "hashed", "admin", now, now)
		mock.ExpectQuery("SELECT .+ FROM users WHERE username").
			WithArgs("alice").
			WillReturnRows(rows)

		req := httptest.NewRequest(http.MethodGet, "/api/users/alice", nil)
		req = mux.SetURLVars(req, map[string]string{"username": "alice"})

		rr := httptest.NewRecorder()
		server.getUserHandler(rr, req)

		require.Equal(t, http.StatusOK, rr.Code)
		require.Contains(t, rr.Body.String(), "alice")
		require.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("not found returns 404", func(t *testing.T) {
		server, mock := newMockServerWithAuth(t)

		mock.ExpectQuery("SELECT .+ FROM users WHERE username").
			WithArgs("nonexistent").
			WillReturnError(sql.ErrNoRows)

		req := httptest.NewRequest(http.MethodGet, "/api/users/nonexistent", nil)
		req = mux.SetURLVars(req, map[string]string{"username": "nonexistent"})

		rr := httptest.NewRecorder()
		server.getUserHandler(rr, req)

		require.Equal(t, http.StatusNotFound, rr.Code)
		require.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("system_admin hidden returns 404", func(t *testing.T) {
		server, mock := newMockServerWithAuth(t)
		now := time.Now().UTC().Truncate(time.Second)

		rows := sqlmock.NewRows([]string{"id", "username", "password_hash", "role", "created_at", "updated_at"}).
			AddRow(1, "admin", "hashed", "system_admin", now, now)
		mock.ExpectQuery("SELECT .+ FROM users WHERE username").
			WithArgs("admin").
			WillReturnRows(rows)

		req := httptest.NewRequest(http.MethodGet, "/api/users/admin", nil)
		req = mux.SetURLVars(req, map[string]string{"username": "admin"})

		rr := httptest.NewRecorder()
		server.getUserHandler(rr, req)

		require.Equal(t, http.StatusNotFound, rr.Code)
		require.NoError(t, mock.ExpectationsWereMet())
	})
}

func TestDeleteUserHandler(t *testing.T) {
	caller := AuthUser{ID: 1, Username: "superadmin", Role: "system_admin"}

	deleteReq := func(username string) *http.Request {
		req := httptest.NewRequest(http.MethodDelete, "/api/users/"+username, nil)
		req = mux.SetURLVars(req, map[string]string{"username": username})
		ctx := context.WithValue(req.Context(), userContextKey, caller)
		return req.WithContext(ctx)
	}

	t.Run("success returns 204", func(t *testing.T) {
		server, mock := newMockServerWithAuth(t)
		now := time.Now().UTC().Truncate(time.Second)

		mock.ExpectQuery("SELECT .+ FROM users WHERE username").
			WithArgs("alice").
			WillReturnRows(sqlmock.NewRows([]string{"id", "username", "password_hash", "role", "created_at", "updated_at"}).
				AddRow(2, "alice", "hashed", "admin", now, now))

		mock.ExpectExec("DELETE FROM sessions").
			WithArgs(int32(2)).
			WillReturnResult(sqlmock.NewResult(0, 1))

		mock.ExpectExec("DELETE FROM users").
			WithArgs("alice").
			WillReturnResult(sqlmock.NewResult(0, 1))

		rr := httptest.NewRecorder()
		server.deleteUserHandler(rr, deleteReq("alice"))

		require.Equal(t, http.StatusNoContent, rr.Code)
		require.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("self-delete blocked returns 400", func(t *testing.T) {
		server, _ := newMockServerWithAuth(t)

		rr := httptest.NewRecorder()
		server.deleteUserHandler(rr, deleteReq("superadmin"))

		require.Equal(t, http.StatusBadRequest, rr.Code)
	})

	t.Run("admin delete blocked returns 400", func(t *testing.T) {
		server, _ := newMockServerWithAuth(t)

		rr := httptest.NewRecorder()
		server.deleteUserHandler(rr, deleteReq("admin"))

		require.Equal(t, http.StatusBadRequest, rr.Code)
	})

	t.Run("not found returns 404", func(t *testing.T) {
		server, mock := newMockServerWithAuth(t)

		mock.ExpectQuery("SELECT .+ FROM users WHERE username").
			WithArgs("nonexistent").
			WillReturnError(sql.ErrNoRows)

		rr := httptest.NewRecorder()
		server.deleteUserHandler(rr, deleteReq("nonexistent"))

		require.Equal(t, http.StatusNotFound, rr.Code)
		require.NoError(t, mock.ExpectationsWereMet())
	})
}

func TestLoginHandler(t *testing.T) {
	t.Run("success returns token", func(t *testing.T) {
		server, mock := newMockServerWithAuth(t)
		now := time.Now().UTC().Truncate(time.Second)

		hash, err := auth.HashPassword("SecurePass1")
		require.NoError(t, err)

		mock.ExpectQuery("SELECT .+ FROM login_attempts").
			WithArgs("testuser").
			WillReturnError(sql.ErrNoRows)

		mock.ExpectQuery("SELECT .+ FROM users WHERE username").
			WithArgs("testuser").
			WillReturnRows(sqlmock.NewRows([]string{"id", "username", "password_hash", "role", "created_at", "updated_at"}).
				AddRow(1, "testuser", hash, "admin", now, now))

		mock.ExpectExec("UPDATE login_attempts").
			WithArgs("testuser").
			WillReturnResult(sqlmock.NewResult(0, 0))

		mock.ExpectQuery("INSERT INTO sessions").
			WithArgs(int32(1), sqlmock.AnyArg(), sqlmock.AnyArg()).
			WillReturnRows(sqlmock.NewRows([]string{"id", "user_id", "token", "expires_at", "created_at"}).
				AddRow(1, 1, "session-token", now.Add(time.Hour), now))

		body, _ := json.Marshal(loginRequest{Username: "testuser", Password: "SecurePass1"})
		req := httptest.NewRequest(http.MethodPost, "/api/auth/login", bytes.NewReader(body))

		rr := httptest.NewRecorder()
		server.loginHandler(rr, req)

		require.Equal(t, http.StatusOK, rr.Code)
		require.Contains(t, rr.Body.String(), "token")
		require.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("wrong password returns 401", func(t *testing.T) {
		server, mock := newMockServerWithAuth(t)
		now := time.Now().UTC().Truncate(time.Second)

		hash, err := auth.HashPassword("CorrectPass1")
		require.NoError(t, err)

		mock.ExpectQuery("SELECT .+ FROM login_attempts").
			WithArgs("testuser").
			WillReturnError(sql.ErrNoRows)

		mock.ExpectQuery("SELECT .+ FROM users WHERE username").
			WithArgs("testuser").
			WillReturnRows(sqlmock.NewRows([]string{"id", "username", "password_hash", "role", "created_at", "updated_at"}).
				AddRow(1, "testuser", hash, "admin", now, now))

		mock.ExpectQuery("INSERT INTO login_attempts").
			WithArgs("testuser").
			WillReturnRows(sqlmock.NewRows([]string{"id", "username", "failed_count", "locked_until", "last_attempt"}).
				AddRow(1, "testuser", int32(1), sql.NullTime{}, now))

		body, _ := json.Marshal(loginRequest{Username: "testuser", Password: "WrongPass1"})
		req := httptest.NewRequest(http.MethodPost, "/api/auth/login", bytes.NewReader(body))

		rr := httptest.NewRecorder()
		server.loginHandler(rr, req)

		require.Equal(t, http.StatusUnauthorized, rr.Code)
		require.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("empty credentials returns 401", func(t *testing.T) {
		server, _ := newMockServerWithAuth(t)

		body, _ := json.Marshal(loginRequest{Username: "", Password: ""})
		req := httptest.NewRequest(http.MethodPost, "/api/auth/login", bytes.NewReader(body))

		rr := httptest.NewRecorder()
		server.loginHandler(rr, req)

		require.Equal(t, http.StatusUnauthorized, rr.Code)
	})
}

func TestLogoutHandler(t *testing.T) {
	t.Run("success returns 204", func(t *testing.T) {
		server, mock := newMockServerWithAuth(t)

		mock.ExpectExec("DELETE FROM sessions").
			WithArgs("valid-token").
			WillReturnResult(sqlmock.NewResult(0, 1))

		req := httptest.NewRequest(http.MethodPost, "/api/auth/logout", nil)
		req.Header.Set("Authorization", "Bearer valid-token")

		rr := httptest.NewRecorder()
		server.logoutHandler(rr, req)

		require.Equal(t, http.StatusNoContent, rr.Code)
		require.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("missing token returns 401", func(t *testing.T) {
		server, _ := newMockServerWithAuth(t)

		req := httptest.NewRequest(http.MethodPost, "/api/auth/logout", nil)

		rr := httptest.NewRecorder()
		server.logoutHandler(rr, req)

		require.Equal(t, http.StatusUnauthorized, rr.Code)
	})
}
