package server

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/goharbor/go-client/pkg/sdk/v2.0/client/robot"
	"github.com/lib/pq"
	"github.com/stretchr/testify/require"
)

type syncGroupRow struct {
	id       int32
	name     string
	projects []string
}

func TestSyncGroupKeepsProjectsFromOtherGroups(t *testing.T) {
	projects, steps := runSyncGroup(t, http.StatusOK, []GroupArtifact{
		{Repository: "store-a/app", Tag: []string{"latest"}},
	}, []syncGroupRow{
		{id: 1, name: "edge-a", projects: []string{"store-a"}},
		{id: 2, name: "edge-b", projects: []string{"store-b"}},
	}, false)

	require.Equal(t, []string{"state", "robot"}, steps)
	require.Equal(t, []string{"store-a", "store-b"}, projects)
}

func TestSyncGroupDropsProjectRemovedFromSyncedGroup(t *testing.T) {
	// store-old used to be on edge-a. This sync marks it deleted, and the
	// group row that comes back no longer lists it.
	projects, steps := runSyncGroup(t, http.StatusOK, []GroupArtifact{
		{Repository: "store-old/app", Tag: []string{"latest"}, Deleted: true},
		{Repository: "store-a/app", Tag: []string{"latest"}},
	}, []syncGroupRow{
		{id: 1, name: "edge-a", projects: []string{"store-a"}},
		{id: 2, name: "edge-b", projects: []string{"store-b"}},
	}, false)

	require.Equal(t, []string{"state", "robot"}, steps)
	require.Equal(t, []string{"store-a", "store-b"}, projects)
	require.NotContains(t, projects, "store-old")
}

func TestSyncGroupDedupesProjectsSharedByGroups(t *testing.T) {
	projects, _ := runSyncGroup(t, http.StatusOK, []GroupArtifact{
		{Repository: "store-a/app", Tag: []string{"latest"}},
		{Repository: "store-b/app", Tag: []string{"latest"}},
	}, []syncGroupRow{
		{id: 1, name: "edge-a", projects: []string{"store-a", "store-b"}},
		{id: 2, name: "edge-b", projects: []string{"store-b", "store-c"}},
	}, false)

	require.Equal(t, []string{"store-a", "store-b", "store-c"}, projects)
}

func TestSyncGroupLeavesRobotsAloneWhenStatePushFails(t *testing.T) {
	projects, steps := runSyncGroup(t, http.StatusInternalServerError, []GroupArtifact{
		{Repository: "store-a/app", Tag: []string{"latest"}},
	}, []syncGroupRow{
		{id: 1, name: "edge-a", projects: []string{"store-a"}},
		{id: 2, name: "edge-b", projects: []string{"store-b"}},
	}, true)

	require.Equal(t, []string{"state"}, steps)
	require.Empty(t, projects)
}

func runSyncGroup(t *testing.T, wantStatus int, artifacts []GroupArtifact, groups []syncGroupRow, failState bool) ([]string, []string) {
	t.Helper()

	server, mock := newMockServer(t)
	now := time.Now().UTC().Truncate(time.Second)

	var steps []string
	var got []string
	restoreSyncHooks(t)
	syncHarborProjectExists = func(context.Context, string) (bool, error) { return true, nil }
	syncCreateStateArtifact = func(context.Context, GroupSyncRequest) error {
		steps = append(steps, "state")
		if failState {
			return errSyncState
		}
		return nil
	}
	syncUpdateRobotProjects = func(_ context.Context, projects []string, _ string) (*robot.UpdateRobotOK, error) {
		steps = append(steps, "robot")
		got = append([]string(nil), projects...)
		return &robot.UpdateRobotOK{}, nil
	}

	mock.ExpectBegin()
	mock.ExpectQuery("INSERT INTO groups").
		WillReturnRows(groupRows(now, groups[0]))
	memberRows := sqlmock.NewRows([]string{"satellite_id", "group_id"}).AddRow(7, groups[0].id)
	mock.ExpectQuery("WHERE group_id").WithArgs(groups[0].id).WillReturnRows(memberRows)

	if !failState {
		mock.ExpectQuery("FROM robot_accounts").
			WithArgs(int32(7)).
			WillReturnRows(sqlmock.NewRows([]string{
				"id", "robot_name", "robot_secret_hash", "robot_id", "satellite_id", "robot_expiry", "created_at", "updated_at",
			}).AddRow(3, "robot$sat", "hash", "99", 7, nil, now, now))
		membership := sqlmock.NewRows([]string{"satellite_id", "group_id"})
		for _, group := range groups {
			membership.AddRow(7, group.id)
		}
		mock.ExpectQuery("WHERE satellite_id").WithArgs(int32(7)).WillReturnRows(membership)
		for _, group := range groups {
			mock.ExpectQuery("WHERE id").WithArgs(group.id).WillReturnRows(groupRows(now, group))
		}
		mock.ExpectCommit()
	} else {
		mock.ExpectRollback()
	}

	body, err := json.Marshal(GroupSyncRequest{Group: groups[0].name, Artifacts: artifacts})
	require.NoError(t, err)
	req := httptest.NewRequest(http.MethodPost, "/api/groups/sync", bytes.NewReader(body))
	rr := httptest.NewRecorder()
	server.SyncGroup(rr, req)

	require.Equal(t, wantStatus, rr.Code)
	require.NoError(t, mock.ExpectationsWereMet())
	return got, steps
}

func groupRows(now time.Time, group syncGroupRow) *sqlmock.Rows {
	return sqlmock.NewRows([]string{"id", "group_name", "registry_url", "projects", "created_at", "updated_at"}).
		AddRow(group.id, group.name, "http://harbor:8080", pq.Array(group.projects), now, now)
}

func restoreSyncHooks(t *testing.T) {
	t.Helper()
	projectExists := syncHarborProjectExists
	createProject := syncCreateSatelliteProject
	createState := syncCreateStateArtifact
	updateRobot := syncUpdateRobotProjects
	t.Cleanup(func() {
		syncHarborProjectExists = projectExists
		syncCreateSatelliteProject = createProject
		syncCreateStateArtifact = createState
		syncUpdateRobotProjects = updateRobot
	})
}

type syncStateError struct{}

func (syncStateError) Error() string { return "state push failed" }

var errSyncState = syncStateError{}
