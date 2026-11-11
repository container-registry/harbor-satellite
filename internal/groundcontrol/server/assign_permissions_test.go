package server

import (
	"context"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/lib/pq"
	"github.com/stretchr/testify/require"
)

func TestAssignPermissionsToRobotKeepsEveryGroup(t *testing.T) {
	server, mock := newMockServer(t)
	fake := &fakeGroupSync{}

	mock.ExpectQuery("SELECT projects FROM groups").WithArgs("edge-a").
		WillReturnRows(sqlmock.NewRows([]string{"projects"}).AddRow(pq.Array([]string{"store-a", "shared"})))
	mock.ExpectQuery("SELECT projects FROM groups").WithArgs("edge-b").
		WillReturnRows(sqlmock.NewRows([]string{"projects"}).AddRow(pq.Array([]string{"shared", "store-b"})))

	err := assignPermissionsToRobot(context.Background(), server.dbQueries, fake, []string{"edge-a", "edge-b"}, 99)
	require.NoError(t, err)

	require.Equal(t, []string{"robot"}, fake.steps)
	require.Equal(t, []string{"store-a", "shared", "store-b"}, fake.projects)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestAssignPermissionsToRobotWithoutGroupsLeavesRobotAlone(t *testing.T) {
	server, mock := newMockServer(t)
	fake := &fakeGroupSync{}

	require.NoError(t, assignPermissionsToRobot(context.Background(), server.dbQueries, fake, nil, 99))

	require.Empty(t, fake.steps)
	require.NoError(t, mock.ExpectationsWereMet())
}
