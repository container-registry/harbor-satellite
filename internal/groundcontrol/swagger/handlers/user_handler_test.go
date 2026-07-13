package handlers

import (
	"database/sql"
	"net/http"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	swaggermodels "github.com/container-registry/harbor-satellite/internal/groundcontrol/swagger/models"
	"github.com/container-registry/harbor-satellite/internal/groundcontrol/swagger/server/operations/users"
	"github.com/go-openapi/strfmt"
	"github.com/stretchr/testify/require"
)

func TestListUsers(t *testing.T) {
	t.Run("returns non-system users", func(t *testing.T) {
		mock := newMockHandlerService(t)
		now := time.Now().UTC().Truncate(time.Second)
		mock.ExpectQuery("SELECT id, username, role, created_at, updated_at FROM users").
			WillReturnRows(sqlmock.NewRows([]string{"id", "username", "role", "created_at", "updated_at"}).
				AddRow(2, "alice", roleAdmin, now, now).
				AddRow(3, "bob", roleAdmin, now, now))

		responder := ListUsers(users.ListUsersParams{
			HTTPRequest: handlerRequest(http.MethodGet, "/api/users"),
		}, handlerTestPrincipal)

		response, ok := responder.(*users.ListUsersOK)
		require.True(t, ok)
		require.Len(t, response.Payload, 2)
		require.Equal(t, "alice", response.Payload[0].Username)
	})

	t.Run("returns an empty array", func(t *testing.T) {
		mock := newMockHandlerService(t)
		mock.ExpectQuery("SELECT id, username, role, created_at, updated_at FROM users").
			WillReturnRows(sqlmock.NewRows([]string{"id", "username", "role", "created_at", "updated_at"}))

		responder := ListUsers(users.ListUsersParams{
			HTTPRequest: handlerRequest(http.MethodGet, "/api/users"),
		}, handlerTestPrincipal)

		response, ok := responder.(*users.ListUsersOK)
		require.True(t, ok)
		require.NotNil(t, response.Payload)
		require.Empty(t, response.Payload)
	})
}

func TestGetUser(t *testing.T) {
	t.Run("returns a matching user", func(t *testing.T) {
		mock := newMockHandlerService(t)
		now := time.Now().UTC().Truncate(time.Second)
		mock.ExpectQuery("SELECT id, username, password_hash, role, created_at, updated_at FROM users").
			WithArgs("alice").
			WillReturnRows(sqlmock.NewRows([]string{"id", "username", "password_hash", "role", "created_at", "updated_at"}).
				AddRow(2, "alice", "hash", roleAdmin, now, now))

		responder := GetUser(users.GetUserParams{
			HTTPRequest: handlerRequest(http.MethodGet, "/api/users/alice"),
			Username:    "alice",
		}, handlerTestPrincipal)

		response, ok := responder.(*users.GetUserOK)
		require.True(t, ok)
		require.Equal(t, "alice", response.Payload.Username)
	})

	t.Run("hides system admins", func(t *testing.T) {
		mock := newMockHandlerService(t)
		now := time.Now().UTC().Truncate(time.Second)
		mock.ExpectQuery("SELECT id, username, password_hash, role, created_at, updated_at FROM users").
			WithArgs("root").
			WillReturnRows(sqlmock.NewRows([]string{"id", "username", "password_hash", "role", "created_at", "updated_at"}).
				AddRow(1, "root", "hash", roleSystemAdmin, now, now))

		responder := GetUser(users.GetUserParams{
			HTTPRequest: handlerRequest(http.MethodGet, "/api/users/root"),
			Username:    "root",
		}, handlerTestPrincipal)

		_, ok := responder.(*users.GetUserNotFound)
		require.True(t, ok)
	})

	t.Run("returns not found", func(t *testing.T) {
		mock := newMockHandlerService(t)
		mock.ExpectQuery("SELECT id, username, password_hash, role, created_at, updated_at FROM users").
			WithArgs("missing").
			WillReturnError(sql.ErrNoRows)

		responder := GetUser(users.GetUserParams{
			HTTPRequest: handlerRequest(http.MethodGet, "/api/users/missing"),
			Username:    "missing",
		}, handlerTestPrincipal)

		_, ok := responder.(*users.GetUserNotFound)
		require.True(t, ok)
	})
}

func TestCreateUserRejectsInvalidInput(t *testing.T) {
	newMockHandlerService(t)
	systemAdmin := principalUser{ID: 1, Username: "root", Role: roleSystemAdmin}
	request := handlerRequest(http.MethodPost, "/api/users")

	_, badBody := CreateUser(users.CreateUserParams{HTTPRequest: request}, systemAdmin).(*users.CreateUserBadRequest)
	require.True(t, badBody)

	reserved := "admin"
	validPassword := strfmt.Password("SecurePass1")
	_, badReserved := CreateUser(users.CreateUserParams{
		HTTPRequest: request,
		Body: &swaggermodels.CreateUserRequest{
			Username: &reserved,
			Password: &validPassword,
		},
	}, systemAdmin).(*users.CreateUserBadRequest)
	require.True(t, badReserved)

	username := "alice"
	weakPassword := strfmt.Password("weak")
	_, badPassword := CreateUser(users.CreateUserParams{
		HTTPRequest: request,
		Body: &swaggermodels.CreateUserRequest{
			Username: &username,
			Password: &weakPassword,
		},
	}, systemAdmin).(*users.CreateUserBadRequest)
	require.True(t, badPassword)
}

func TestDeleteUserRejectsProtectedAccounts(t *testing.T) {
	newMockHandlerService(t)
	systemAdmin := principalUser{ID: 1, Username: "root", Role: roleSystemAdmin}

	_, selfDelete := DeleteUser(users.DeleteUserParams{
		HTTPRequest: handlerRequest(http.MethodDelete, "/api/users/root"),
		Username:    "root",
	}, systemAdmin).(*users.DeleteUserBadRequest)
	require.True(t, selfDelete)

	_, adminDelete := DeleteUser(users.DeleteUserParams{
		HTTPRequest: handlerRequest(http.MethodDelete, "/api/users/admin"),
		Username:    "admin",
	}, systemAdmin).(*users.DeleteUserBadRequest)
	require.True(t, adminDelete)
}

func TestPasswordHandlersRejectInvalidInput(t *testing.T) {
	newMockHandlerService(t)
	request := handlerRequest(http.MethodPatch, "/api/users/password")

	_, missingOwnBody := ChangeOwnPassword(users.ChangeOwnPasswordParams{HTTPRequest: request}, handlerTestPrincipal).(*users.ChangeOwnPasswordBadRequest)
	require.True(t, missingOwnBody)

	currentPassword := strfmt.Password("CurrentPass1")
	weakPassword := strfmt.Password("weak")
	_, weakOwnPassword := ChangeOwnPassword(users.ChangeOwnPasswordParams{
		HTTPRequest: request,
		Body: &swaggermodels.ChangePasswordRequest{
			CurrentPassword: &currentPassword,
			NewPassword:     &weakPassword,
		},
	}, handlerTestPrincipal).(*users.ChangeOwnPasswordBadRequest)
	require.True(t, weakOwnPassword)

	systemAdmin := principalUser{ID: 1, Username: "root", Role: roleSystemAdmin}
	_, missingResetBody := ChangeUserPassword(users.ChangeUserPasswordParams{
		HTTPRequest: request,
		Username:    "alice",
	}, systemAdmin).(*users.ChangeUserPasswordBadRequest)
	require.True(t, missingResetBody)

	_, weakResetPassword := ChangeUserPassword(users.ChangeUserPasswordParams{
		HTTPRequest: request,
		Username:    "alice",
		Body:        &swaggermodels.ChangeUserPasswordRequest{NewPassword: &weakPassword},
	}, systemAdmin).(*users.ChangeUserPasswordBadRequest)
	require.True(t, weakResetPassword)

	_, forbidden := ChangeUserPassword(users.ChangeUserPasswordParams{
		HTTPRequest: request,
		Username:    "alice",
	}, handlerTestPrincipal).(*users.ChangeUserPasswordForbidden)
	require.True(t, forbidden)
}
