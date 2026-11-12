package server

import (
	"context"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/lib/pq"
	"github.com/stretchr/testify/require"
)

func TestSyncRobotPermissionsUpdatesEachRobotOnce(t *testing.T) {
	server, mock := newMockServer(t)
	fake := &fakeGroupSync{}
	mock.ExpectQuery("FROM robot_accounts").WithArgs(sqlmock.AnyArg()).
		WillReturnRows(sqlmock.NewRows([]string{"satellite_id", "robot_id", "robot_name", "projects"}).
			AddRow(7, "99", "robot$sat-7", pq.Array([]string{"shared", "store-a", "store-b"})))

	require.NoError(t, syncRobotPermissionsWith(context.Background(), server.dbQueries, fake, []int32{7}))

	require.Equal(t, []string{"robot"}, fake.steps)
	require.Equal(t, []string{"shared", "store-a", "store-b"}, fake.projects)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestSyncRobotPermissionsWithoutSatellitesLeavesRobotsAlone(t *testing.T) {
	server, mock := newMockServer(t)
	fake := &fakeGroupSync{}

	require.NoError(t, syncRobotPermissionsWith(context.Background(), server.dbQueries, fake, nil))

	require.Empty(t, fake.steps)
	require.NoError(t, mock.ExpectationsWereMet())
}
