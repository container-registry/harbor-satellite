package handlers

import (
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	gcauth "github.com/container-registry/harbor-satellite/internal/groundcontrol/auth"
	"github.com/container-registry/harbor-satellite/internal/groundcontrol/database"
	"github.com/stretchr/testify/require"
)

var handlerTestPrincipal = principalUser{ID: 1, Username: "operator", Role: roleAdmin}

func newMockHandlerService(t *testing.T) sqlmock.Sqlmock {
	t.Helper()

	db, mock, err := sqlmock.New(sqlmock.MonitorPingsOption(true))
	require.NoError(t, err)

	serviceOnce = sync.Once{}
	serviceInst = nil
	errService = nil
	serviceOnce.Do(func() {
		serviceInst = &service{
			db:              db,
			queries:         database.New(db),
			passwordPolicy:  gcauth.DefaultPolicy(),
			sessionDuration: 24 * time.Hour,
			lockoutDuration: 5 * time.Minute,
		}
	})

	t.Cleanup(func() {
		mock.ExpectClose()
		require.NoError(t, db.Close())
		require.NoError(t, mock.ExpectationsWereMet())
		serviceOnce = sync.Once{}
		serviceInst = nil
		errService = nil
	})

	return mock
}

func handlerRequest(method, target string) *http.Request {
	return httptest.NewRequest(method, target, nil)
}
