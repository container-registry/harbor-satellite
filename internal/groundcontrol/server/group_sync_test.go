package server

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"slices"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
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

	fake := &fakeGroupSync{failState: failState}
	server.groupSync = fake

	mock.ExpectBegin()
	mock.ExpectExec("pg_advisory_xact_lock").WithArgs(membershipLockID).WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectQuery("INSERT INTO groups").
		WillReturnRows(groupRows(now, groups[0]))
	memberRows := sqlmock.NewRows([]string{"satellite_id", "group_id"}).AddRow(7, groups[0].id)
	mock.ExpectQuery("WHERE group_id").WithArgs(groups[0].id).WillReturnRows(memberRows)

	if !failState {
		// Postgres computes the union (GetProjectsOfSatellites); the
		// integration tests in robot_permissions_test.go cover that query.
		mock.ExpectQuery("FROM robot_accounts").WithArgs(sqlmock.AnyArg()).
			WillReturnRows(sqlmock.NewRows([]string{"satellite_id", "robot_id", "robot_name", "projects"}).
				AddRow(7, "99", "robot$sat", pq.Array(unionOfProjects(groups))))
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
	return fake.projects, fake.steps
}

func unionOfProjects(groups []syncGroupRow) []string {
	var all []string
	for _, g := range groups {
		all = append(all, g.projects...)
	}
	slices.Sort(all)
	return slices.Compact(all)
}

func groupRows(now time.Time, group syncGroupRow) *sqlmock.Rows {
	return sqlmock.NewRows([]string{"id", "group_name", "registry_url", "projects", "created_at", "updated_at"}).
		AddRow(group.id, group.name, "http://harbor:8080", pq.Array(group.projects), now, now)
}

// fakeGroupSync records the Harbor calls SyncGroup makes.
type fakeGroupSync struct {
	failState bool
	steps     []string
	projects  []string
}

func (*fakeGroupSync) ProjectExists(context.Context, string) (bool, error) {
	return true, nil
}

func (*fakeGroupSync) CreateSatelliteProject(context.Context) (bool, error) {
	return true, nil
}

func (f *fakeGroupSync) CreateStateArtifact(context.Context, GroupSyncRequest) error {
	f.steps = append(f.steps, "state")
	if f.failState {
		return errors.New("state push failed")
	}
	return nil
}

func (f *fakeGroupSync) UpdateRobotProjects(_ context.Context, projects []string, _ string) error {
	f.steps = append(f.steps, "robot")
	f.projects = append([]string(nil), projects...)
	return nil
}
