package server

import (
	"bytes"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/stretchr/testify/require"

	"github.com/container-registry/harbor-satellite/ground-control/internal/auth"
	"github.com/container-registry/harbor-satellite/ground-control/internal/database"
	internalmiddleware "github.com/container-registry/harbor-satellite/ground-control/internal/middleware"
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
		rateLimiter:     internalmiddleware.NewRateLimiter(10, time.Minute),
		passwordPolicy:  auth.DefaultPolicy(),
		sessionDuration: time.Hour,
		lockoutDuration: 15 * time.Minute,
	}, mock
}

func TestGeneratedUserRoutes(t *testing.T) {
	t.Run("create user", func(t *testing.T) {
		server, mock := newMockServerWithAuth(t)
		now := time.Now().UTC().Truncate(time.Second)

		expectBasicAuth(mock, "superadmin", "SecurePass1", roleSystemAdmin, now)
		mock.ExpectQuery("INSERT INTO users").
			WithArgs("testuser", sqlmock.AnyArg(), roleAdmin).
			WillReturnRows(sqlmock.NewRows([]string{"id", "username", "password_hash", "role", "created_at", "updated_at"}).
				AddRow(2, "testuser", "hashed", roleAdmin, now, now))

		body, err := json.Marshal(createUserRequest{Username: "testuser", Password: "SecurePass1"})
		require.NoError(t, err)

		req := httptest.NewRequest(http.MethodPost, "/api/users", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		setBasicAuth(req, "superadmin", "SecurePass1")

		rr := httptest.NewRecorder()
		server.RegisterRoutes().ServeHTTP(rr, req)

		require.Equal(t, http.StatusCreated, rr.Code)
		require.Contains(t, rr.Body.String(), "testuser")
		require.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("list users", func(t *testing.T) {
		server, mock := newMockServerWithAuth(t)
		now := time.Now().UTC().Truncate(time.Second)

		expectBasicAuth(mock, "alice", "SecurePass1", roleAdmin, now)
		rows := sqlmock.NewRows([]string{"id", "username", "role", "created_at", "updated_at"}).
			AddRow(1, "alice", roleAdmin, now, now).
			AddRow(2, "bob", roleAdmin, now, now)
		mock.ExpectQuery("SELECT .+ FROM users").WillReturnRows(rows)

		req := httptest.NewRequest(http.MethodGet, "/api/users", nil)
		setBasicAuth(req, "alice", "SecurePass1")

		rr := httptest.NewRecorder()
		server.RegisterRoutes().ServeHTTP(rr, req)

		require.Equal(t, http.StatusOK, rr.Code)
		require.Contains(t, rr.Body.String(), "alice")
		require.Contains(t, rr.Body.String(), "bob")
		require.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("get user not found", func(t *testing.T) {
		server, mock := newMockServerWithAuth(t)
		now := time.Now().UTC().Truncate(time.Second)

		expectBasicAuth(mock, "alice", "SecurePass1", roleAdmin, now)
		mock.ExpectQuery("SELECT .+ FROM users WHERE username").
			WithArgs("missing").
			WillReturnError(sql.ErrNoRows)

		req := httptest.NewRequest(http.MethodGet, "/api/users/missing", nil)
		setBasicAuth(req, "alice", "SecurePass1")

		rr := httptest.NewRecorder()
		server.RegisterRoutes().ServeHTTP(rr, req)

		require.Equal(t, http.StatusNotFound, rr.Code)
		require.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("delete user", func(t *testing.T) {
		server, mock := newMockServerWithAuth(t)
		now := time.Now().UTC().Truncate(time.Second)

		expectBasicAuth(mock, "superadmin", "SecurePass1", roleSystemAdmin, now)
		mock.ExpectQuery("SELECT .+ FROM users WHERE username").
			WithArgs("alice").
			WillReturnRows(sqlmock.NewRows([]string{"id", "username", "password_hash", "role", "created_at", "updated_at"}).
				AddRow(2, "alice", "hashed", roleAdmin, now, now))
		mock.ExpectExec("DELETE FROM sessions").WithArgs(int32(2)).WillReturnResult(sqlmock.NewResult(0, 1))
		mock.ExpectExec("DELETE FROM users").WithArgs("alice").WillReturnResult(sqlmock.NewResult(0, 1))

		req := httptest.NewRequest(http.MethodDelete, "/api/users/alice", nil)
		setBasicAuth(req, "superadmin", "SecurePass1")

		rr := httptest.NewRecorder()
		server.RegisterRoutes().ServeHTTP(rr, req)

		require.Equal(t, http.StatusNoContent, rr.Code)
		require.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("change own password", func(t *testing.T) {
		server, mock := newMockServerWithAuth(t)
		now := time.Now().UTC().Truncate(time.Second)
		hash, err := auth.HashPassword("CurrentPass1")
		require.NoError(t, err)

		expectBasicAuthWithHash(mock, "alice", hash, roleAdmin, now)
		mock.ExpectQuery("SELECT .+ FROM users WHERE username").
			WithArgs("alice").
			WillReturnRows(sqlmock.NewRows([]string{"id", "username", "password_hash", "role", "created_at", "updated_at"}).
				AddRow(1, "alice", hash, roleAdmin, now, now))
		mock.ExpectExec("UPDATE users SET password_hash").WithArgs("alice", sqlmock.AnyArg()).WillReturnResult(sqlmock.NewResult(0, 1))
		mock.ExpectExec("DELETE FROM sessions").WithArgs(int32(1)).WillReturnResult(sqlmock.NewResult(0, 1))

		body, err := json.Marshal(changePasswordRequest{CurrentPassword: "CurrentPass1", NewPassword: "SecurePass2"})
		require.NoError(t, err)

		req := httptest.NewRequest(http.MethodPatch, "/api/users/password", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		setBasicAuth(req, "alice", "CurrentPass1")

		rr := httptest.NewRecorder()
		server.RegisterRoutes().ServeHTTP(rr, req)

		require.Equal(t, http.StatusNoContent, rr.Code)
		require.NoError(t, mock.ExpectationsWereMet())
	})
}

func TestGeneratedAuthRoutes(t *testing.T) {
	t.Run("login", func(t *testing.T) {
		server, mock := newMockServerWithAuth(t)
		now := time.Now().UTC().Truncate(time.Second)
		hash, err := auth.HashPassword("SecurePass1")
		require.NoError(t, err)

		mock.ExpectQuery("SELECT .+ FROM login_attempts").WithArgs("testuser").WillReturnError(sql.ErrNoRows)
		mock.ExpectQuery("SELECT .+ FROM users WHERE username").
			WithArgs("testuser").
			WillReturnRows(sqlmock.NewRows([]string{"id", "username", "password_hash", "role", "created_at", "updated_at"}).
				AddRow(1, "testuser", hash, roleAdmin, now, now))
		mock.ExpectExec("UPDATE login_attempts").WithArgs("testuser").WillReturnResult(sqlmock.NewResult(0, 0))
		mock.ExpectQuery("INSERT INTO sessions").
			WithArgs(int32(1), sqlmock.AnyArg(), sqlmock.AnyArg()).
			WillReturnRows(sqlmock.NewRows([]string{"id", "user_id", "token", "expires_at", "created_at"}).
				AddRow(1, 1, "session-token", now.Add(time.Hour), now))

		body, err := json.Marshal(loginRequest{Username: "testuser", Password: "SecurePass1"})
		require.NoError(t, err)

		req := httptest.NewRequest(http.MethodPost, "/login", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		rr := httptest.NewRecorder()
		server.RegisterRoutes().ServeHTTP(rr, req)

		require.Equal(t, http.StatusOK, rr.Code)
		require.Contains(t, rr.Body.String(), "token")
		require.NoError(t, mock.ExpectationsWereMet())
	})
}

func expectBasicAuth(mock sqlmock.Sqlmock, username, password, role string, now time.Time) {
	hash, err := auth.HashPassword(password)
	if err != nil {
		panic(err)
	}

	expectBasicAuthWithHash(mock, username, hash, role, now)
}

func expectBasicAuthWithHash(mock sqlmock.Sqlmock, username, hash, role string, now time.Time) {
	mock.ExpectQuery("SELECT .+ FROM users WHERE username").
		WithArgs(username).
		WillReturnRows(sqlmock.NewRows([]string{"id", "username", "password_hash", "role", "created_at", "updated_at"}).
			AddRow(1, username, hash, role, now, now))
}

func setBasicAuth(req *http.Request, username, password string) {
	credentials := base64.StdEncoding.EncodeToString([]byte(fmt.Sprintf("%s:%s", username, password)))
	req.Header.Set("Authorization", "Basic "+credentials)
}
