package handlers

import (
	"context"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/container-registry/harbor-satellite/internal/env"
	"github.com/stretchr/testify/require"
)

func TestBootstrapSystemAdmin(t *testing.T) {
	mock := newMockHandlerService(t)
	originalPassword := env.GC.Server.AdminPassword
	env.GC.Server.AdminPassword = "SecurePass1"
	t.Cleanup(func() { env.GC.Server.AdminPassword = originalPassword })

	now := time.Now().UTC().Truncate(time.Second)
	mock.ExpectQuery("SELECT EXISTS").
		WillReturnRows(sqlmock.NewRows([]string{"exists"}).AddRow(false))
	mock.ExpectQuery("INSERT INTO users").
		WithArgs(systemAdminUsername, sqlmock.AnyArg(), roleSystemAdmin).
		WillReturnRows(sqlmock.NewRows([]string{"id", "username", "password_hash", "role", "created_at", "updated_at"}).
			AddRow(1, systemAdminUsername, "hash", roleSystemAdmin, now, now))

	require.NoError(t, serviceInst.bootstrapSystemAdmin(context.Background()))
}

func TestBootstrapSystemAdminDoesNotResetExistingAdmin(t *testing.T) {
	mock := newMockHandlerService(t)
	mock.ExpectQuery("SELECT EXISTS").
		WillReturnRows(sqlmock.NewRows([]string{"exists"}).AddRow(true))

	require.NoError(t, serviceInst.bootstrapSystemAdmin(context.Background()))
}
