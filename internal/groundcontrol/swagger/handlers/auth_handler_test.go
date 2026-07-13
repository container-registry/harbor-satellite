package handlers

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	gcauth "github.com/container-registry/harbor-satellite/internal/groundcontrol/auth"
	auditlog "github.com/container-registry/harbor-satellite/internal/groundcontrol/logger"
	swaggermodels "github.com/container-registry/harbor-satellite/internal/groundcontrol/swagger/models"
	swaggerauth "github.com/container-registry/harbor-satellite/internal/groundcontrol/swagger/server/operations/auth"
	"github.com/go-openapi/strfmt"
	"github.com/stretchr/testify/require"
)

func TestSessionTokenAcceptsRawAndBearerAuthorization(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		header string
		want   string
	}{
		"raw":          {header: "session-token", want: "session-token"},
		"raw base64":   {header: "xRFFq2cYfRNopKlBOZgFwCbAU4qVhjY71DmmXWwP2DtimuS2di4D58ANrTNI4aw3q49eTYnH4ZEb_iAKHPWQaA==", want: "xRFFq2cYfRNopKlBOZgFwCbAU4qVhjY71DmmXWwP2DtimuS2di4D58ANrTNI4aw3q49eTYnH4ZEb_iAKHPWQaA=="},
		"canonical":    {header: "Bearer session-token", want: "session-token"},
		"lowercase":    {header: "bearer session-token", want: "session-token"},
		"mixed case":   {header: "BeArEr session-token", want: "session-token"},
		"extra spaces": {header: "  Bearer   session-token  ", want: "session-token"},
		"missing":      {header: "", want: ""},
		"empty bearer": {header: "Bearer ", want: ""},
		"other scheme": {header: "Basic session-token", want: ""},
		"too many":     {header: "Bearer session-token extra", want: ""},
	}

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			if got := sessionToken(test.header); got != test.want {
				t.Fatalf("sessionToken(%q) = %q, want %q", test.header, got, test.want)
			}
		})
	}
}

func TestLoginRejectsInvalidInput(t *testing.T) {
	newMockHandlerService(t)
	request := handlerRequest(http.MethodPost, "/login")

	_, badBody := Login(swaggerauth.LoginParams{HTTPRequest: request}).(*swaggerauth.LoginBadRequest)
	require.True(t, badBody)

	username := ""
	password := strfmt.Password("")
	_, unauthorized := Login(swaggerauth.LoginParams{
		HTTPRequest: request,
		Body: &swaggermodels.LoginRequest{
			Username: &username,
			Password: &password,
		},
	}).(*swaggerauth.LoginUnauthorized)
	require.True(t, unauthorized)
}

func TestLogout(t *testing.T) {
	t.Run("requires an authorization header", func(t *testing.T) {
		newMockHandlerService(t)

		responder := Logout(swaggerauth.LogoutParams{
			HTTPRequest: handlerRequest(http.MethodPost, "/api/logout"),
		}, handlerTestPrincipal)

		_, ok := responder.(*swaggerauth.LogoutUnauthorized)
		require.True(t, ok)
	})

	t.Run("deletes the current session", func(t *testing.T) {
		mock := newMockHandlerService(t)
		mock.ExpectExec("DELETE FROM sessions").
			WithArgs("session-token").
			WillReturnResult(sqlmock.NewResult(0, 1))
		request := handlerRequest(http.MethodPost, "/api/logout")
		request.Header.Set("Authorization", "session-token")

		responder := Logout(swaggerauth.LogoutParams{HTTPRequest: request}, handlerTestPrincipal)

		_, ok := responder.(*swaggerauth.LogoutNoContent)
		require.True(t, ok)
	})
}

func TestLogin(t *testing.T) {
	t.Run("creates a session", func(t *testing.T) {
		mock := newMockHandlerService(t)
		now := time.Now().UTC().Truncate(time.Second)
		hash, err := gcauth.HashPassword("SecurePass1")
		require.NoError(t, err)
		mock.ExpectQuery("SELECT id, username, failed_count, locked_until, last_attempt FROM login_attempts").
			WithArgs("alice").
			WillReturnError(sql.ErrNoRows)
		mock.ExpectQuery("SELECT id, username, password_hash, role, created_at, updated_at FROM users").
			WithArgs("alice").
			WillReturnRows(sqlmock.NewRows([]string{"id", "username", "password_hash", "role", "created_at", "updated_at"}).
				AddRow(2, "alice", hash, roleAdmin, now, now))
		mock.ExpectExec("UPDATE login_attempts").
			WithArgs("alice").
			WillReturnResult(sqlmock.NewResult(0, 0))
		mock.ExpectQuery("INSERT INTO sessions").
			WithArgs(int32(2), sqlmock.AnyArg(), sqlmock.AnyArg()).
			WillReturnRows(sqlmock.NewRows([]string{"id", "user_id", "token", "expires_at", "created_at"}).
				AddRow(10, 2, "stored-token", now.Add(24*time.Hour), now))
		username := "alice"
		password := strfmt.Password("SecurePass1")

		responder := Login(swaggerauth.LoginParams{
			HTTPRequest: handlerRequest(http.MethodPost, "/login"),
			Body: &swaggermodels.LoginRequest{
				Username: &username,
				Password: &password,
			},
		})

		response, ok := responder.(*swaggerauth.LoginOK)
		require.True(t, ok)
		require.NotEmpty(t, response.Payload.Token)
		require.True(t, time.Time(response.Payload.ExpiresAt).After(now))
	})

	t.Run("does not create a session when attempt reset fails", func(t *testing.T) {
		mock := newMockHandlerService(t)
		now := time.Now().UTC().Truncate(time.Second)
		hash, err := gcauth.HashPassword("SecurePass1")
		require.NoError(t, err)
		mock.ExpectQuery("SELECT id, username, failed_count, locked_until, last_attempt FROM login_attempts").
			WithArgs("alice").
			WillReturnError(sql.ErrNoRows)
		mock.ExpectQuery("SELECT id, username, password_hash, role, created_at, updated_at FROM users").
			WithArgs("alice").
			WillReturnRows(sqlmock.NewRows([]string{"id", "username", "password_hash", "role", "created_at", "updated_at"}).
				AddRow(2, "alice", hash, roleAdmin, now, now))
		mock.ExpectExec("UPDATE login_attempts").
			WithArgs("alice").
			WillReturnError(errors.New("database unavailable"))
		username := "alice"
		password := strfmt.Password("SecurePass1")

		responder := Login(swaggerauth.LoginParams{
			HTTPRequest: handlerRequest(http.MethodPost, "/login"),
			Body: &swaggermodels.LoginRequest{
				Username: &username,
				Password: &password,
			},
		})

		response, ok := responder.(*swaggerauth.LoginInternalServerError)
		require.True(t, ok)
		require.Contains(t, response.Payload.Message, "reset login attempt state")
	})

	t.Run("records a failed password", func(t *testing.T) {
		mock := newMockHandlerService(t)
		auditPath := attachHandlerAuditLogger(t)
		now := time.Now().UTC().Truncate(time.Second)
		hash, err := gcauth.HashPassword("SecurePass1")
		require.NoError(t, err)
		mock.ExpectQuery("SELECT id, username, failed_count, locked_until, last_attempt FROM login_attempts").
			WithArgs("alice").
			WillReturnError(sql.ErrNoRows)
		mock.ExpectQuery("SELECT id, username, password_hash, role, created_at, updated_at FROM users").
			WithArgs("alice").
			WillReturnRows(sqlmock.NewRows([]string{"id", "username", "password_hash", "role", "created_at", "updated_at"}).
				AddRow(2, "alice", hash, roleAdmin, now, now))
		mock.ExpectQuery("INSERT INTO login_attempts").
			WithArgs("alice").
			WillReturnRows(sqlmock.NewRows([]string{"id", "username", "failed_count", "locked_until", "last_attempt"}).
				AddRow(1, "alice", 1, sql.NullTime{}, now))
		username := "alice"
		password := strfmt.Password("WrongPass1")

		request := handlerRequest(http.MethodPost, "/login")
		request.Header.Set("X-Request-ID", "login-test-request")
		responder := Login(swaggerauth.LoginParams{
			HTTPRequest: request,
			Body: &swaggermodels.LoginRequest{
				Username: &username,
				Password: &password,
			},
		})

		_, ok := responder.(*swaggerauth.LoginUnauthorized)
		require.True(t, ok)

		event := readHandlerAuditEvent(t, auditPath)
		require.Equal(t, "session.login.failure", event["event_type"])
		require.Equal(t, "bad_password", event["reason"])
		require.Equal(t, "alice", event["actor"])
		require.Equal(t, "login-test-request", event["request_id"])
	})
}

func TestAuthenticateBearerAcceptsRawToken(t *testing.T) {
	mock := newMockHandlerService(t)
	now := time.Now().UTC().Truncate(time.Second)
	mock.ExpectQuery("SELECT s.id, s.user_id, s.token, s.expires_at, s.created_at, u.username, u.role").
		WithArgs("session-token").
		WillReturnRows(sqlmock.NewRows([]string{"id", "user_id", "token", "expires_at", "created_at", "username", "role"}).
			AddRow(10, 2, "session-token", now.Add(time.Hour), now, "alice", roleAdmin))

	principal, err := AuthenticateBearer("session-token")
	require.NoError(t, err)
	require.Equal(t, principalUser{ID: 2, Username: "alice", Role: roleAdmin}, principal)
}

func attachHandlerAuditLogger(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "audit.log")
	logger, err := auditlog.NewAuditLogger(auditlog.AuditConfig{
		Enabled: true,
		Syslog: auditlog.SyslogConfig{
			Enabled: true,
			Target:  auditlog.SyslogTargetFile,
			File:    auditlog.SyslogFileConfig{Path: path},
		},
	}, auditlog.ComponentGroundControl)
	require.NoError(t, err)
	serviceInst.audit = logger
	t.Cleanup(func() { require.NoError(t, logger.Close()) })
	return path
}

func readHandlerAuditEvent(t *testing.T, path string) map[string]any {
	t.Helper()
	data, err := os.ReadFile(path)
	require.NoError(t, err)
	brace := bytes.IndexByte(data, '{')
	require.GreaterOrEqual(t, brace, 0)
	var event map[string]any
	require.NoError(t, json.Unmarshal(data[brace:], &event))
	return event
}
