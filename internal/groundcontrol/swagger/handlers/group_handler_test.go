package handlers

import (
	"database/sql"
	"errors"
	"net/http"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	swaggermodels "github.com/container-registry/harbor-satellite/internal/groundcontrol/swagger/models"
	"github.com/container-registry/harbor-satellite/internal/groundcontrol/swagger/server/operations/groups"
	"github.com/lib/pq"
	"github.com/stretchr/testify/require"
)

func TestListGroups(t *testing.T) {
	t.Run("returns groups", func(t *testing.T) {
		mock := newMockHandlerService(t)
		now := time.Now().UTC().Truncate(time.Second)
		rows := sqlmock.NewRows([]string{"id", "group_name", "registry_url", "projects", "created_at", "updated_at"}).
			AddRow(1, "edge", "https://registry.example", pq.Array([]string{"edge-a"}), now, now).
			AddRow(2, "factory", "https://registry.example", pq.Array([]string{"factory-a", "factory-b"}), now, now)
		mock.ExpectQuery("SELECT id, group_name, registry_url, projects, created_at, updated_at FROM groups").WillReturnRows(rows)

		responder := ListGroups(groups.ListGroupsParams{
			HTTPRequest: handlerRequest(http.MethodGet, "/api/groups"),
		}, handlerTestPrincipal)

		response, ok := responder.(*groups.ListGroupsOK)
		require.True(t, ok)
		require.Len(t, response.Payload, 2)
		require.Equal(t, []string{"factory-a", "factory-b"}, response.Payload[1].Projects)
	})

	t.Run("returns an empty array", func(t *testing.T) {
		mock := newMockHandlerService(t)
		mock.ExpectQuery("SELECT id, group_name, registry_url, projects, created_at, updated_at FROM groups").
			WillReturnRows(sqlmock.NewRows([]string{"id", "group_name", "registry_url", "projects", "created_at", "updated_at"}))

		responder := ListGroups(groups.ListGroupsParams{
			HTTPRequest: handlerRequest(http.MethodGet, "/api/groups"),
		}, handlerTestPrincipal)

		response, ok := responder.(*groups.ListGroupsOK)
		require.True(t, ok)
		require.NotNil(t, response.Payload)
		require.Empty(t, response.Payload)
	})

	t.Run("reports database failures", func(t *testing.T) {
		mock := newMockHandlerService(t)
		mock.ExpectQuery("SELECT id, group_name, registry_url, projects, created_at, updated_at FROM groups").
			WillReturnError(errors.New("database unavailable"))

		responder := ListGroups(groups.ListGroupsParams{
			HTTPRequest: handlerRequest(http.MethodGet, "/api/groups"),
		}, handlerTestPrincipal)

		_, ok := responder.(*groups.ListGroupsInternalServerError)
		require.True(t, ok)
	})
}

func TestGetGroup(t *testing.T) {
	t.Run("returns a matching group", func(t *testing.T) {
		mock := newMockHandlerService(t)
		now := time.Now().UTC().Truncate(time.Second)
		mock.ExpectQuery("SELECT id, group_name, registry_url, projects, created_at, updated_at FROM groups").
			WithArgs("edge").
			WillReturnRows(sqlmock.NewRows([]string{"id", "group_name", "registry_url", "projects", "created_at", "updated_at"}).
				AddRow(1, "edge", "https://registry.example", pq.Array([]string{"edge-a"}), now, now))

		responder := GetGroup(groups.GetGroupParams{
			HTTPRequest: handlerRequest(http.MethodGet, "/api/groups/edge"),
			Group:       "edge",
		}, handlerTestPrincipal)

		response, ok := responder.(*groups.GetGroupOK)
		require.True(t, ok)
		require.Equal(t, "edge", response.Payload.GroupName)
	})

	t.Run("returns not found", func(t *testing.T) {
		mock := newMockHandlerService(t)
		mock.ExpectQuery("SELECT id, group_name, registry_url, projects, created_at, updated_at FROM groups").
			WithArgs("missing").
			WillReturnError(sql.ErrNoRows)

		responder := GetGroup(groups.GetGroupParams{
			HTTPRequest: handlerRequest(http.MethodGet, "/api/groups/missing"),
			Group:       "missing",
		}, handlerTestPrincipal)

		_, ok := responder.(*groups.GetGroupNotFound)
		require.True(t, ok)
	})
}

func TestListGroupSatellites(t *testing.T) {
	t.Run("returns group satellites", func(t *testing.T) {
		mock := newMockHandlerService(t)
		now := time.Now().UTC().Truncate(time.Second)
		mock.ExpectQuery("SELECT id, group_name, registry_url, projects, created_at, updated_at FROM groups").
			WithArgs("edge").
			WillReturnRows(sqlmock.NewRows([]string{"id", "group_name", "registry_url", "projects", "created_at", "updated_at"}).
				AddRow(1, "edge", "https://registry.example", pq.Array([]string{"edge-a"}), now, now))
		mock.ExpectQuery("SELECT s.id, s.name, s.created_at, s.updated_at").
			WithArgs("edge").
			WillReturnRows(sqlmock.NewRows([]string{"id", "name", "created_at", "updated_at"}).
				AddRow(4, "sat-01", now, now).
				AddRow(5, "sat-02", now, now))

		responder := ListGroupSatellites(groups.ListGroupSatellitesParams{
			HTTPRequest: handlerRequest(http.MethodGet, "/api/groups/edge/satellites"),
			Group:       "edge",
		}, handlerTestPrincipal)

		response, ok := responder.(*groups.ListGroupSatellitesOK)
		require.True(t, ok)
		require.Len(t, response.Payload, 2)
		require.Equal(t, "sat-02", response.Payload[1].Name)
	})

	t.Run("requires an existing group", func(t *testing.T) {
		mock := newMockHandlerService(t)
		mock.ExpectQuery("SELECT id, group_name, registry_url, projects, created_at, updated_at FROM groups").
			WithArgs("missing").
			WillReturnError(sql.ErrNoRows)

		responder := ListGroupSatellites(groups.ListGroupSatellitesParams{
			HTTPRequest: handlerRequest(http.MethodGet, "/api/groups/missing/satellites"),
			Group:       "missing",
		}, handlerTestPrincipal)

		_, ok := responder.(*groups.ListGroupSatellitesNotFound)
		require.True(t, ok)
	})
}

func TestDeleteGroupRequiresSystemAdmin(t *testing.T) {
	newMockHandlerService(t)
	params := groups.DeleteGroupParams{
		HTTPRequest: handlerRequest(http.MethodDelete, "/api/groups/edge"),
		Group:       "edge",
	}

	_, unauthorized := DeleteGroup(params, nil).(*groups.DeleteGroupUnauthorized)
	require.True(t, unauthorized)

	_, forbidden := DeleteGroup(params, handlerTestPrincipal).(*groups.DeleteGroupForbidden)
	require.True(t, forbidden)
}

func TestAddSatelliteToGroup(t *testing.T) {
	t.Run("rejects invalid input", func(t *testing.T) {
		newMockHandlerService(t)
		request := handlerRequest(http.MethodPost, "/api/groups/satellite")

		_, missingBody := AddSatelliteToGroup(groups.AddSatelliteToGroupParams{HTTPRequest: request}, handlerTestPrincipal).(*groups.AddSatelliteToGroupBadRequest)
		require.True(t, missingBody)

		_, invalidName := AddSatelliteToGroup(groups.AddSatelliteToGroupParams{
			HTTPRequest: request,
			Body:        &swaggermodels.SatelliteGroupParams{Satellite: "Invalid Name", Group: "edge"},
		}, handlerTestPrincipal).(*groups.AddSatelliteToGroupBadRequest)
		require.True(t, invalidName)
	})

	t.Run("reports an existing membership", func(t *testing.T) {
		mock := newMockHandlerService(t)
		now := time.Now().UTC().Truncate(time.Second)
		mock.ExpectQuery("SELECT id, name, created_at, updated_at, last_seen, heartbeat_interval FROM satellites").
			WithArgs("edge-01").
			WillReturnRows(sqlmock.NewRows([]string{"id", "name", "created_at", "updated_at", "last_seen", "heartbeat_interval"}).
				AddRow(1, "edge-01", now, now, sql.NullTime{}, sql.NullString{}))
		mock.ExpectQuery("SELECT id, group_name, registry_url, projects, created_at, updated_at FROM groups").
			WithArgs("edge").
			WillReturnRows(sqlmock.NewRows([]string{"id", "group_name", "registry_url", "projects", "created_at", "updated_at"}).
				AddRow(2, "edge", "https://registry.example", pq.Array([]string{"edge-a"}), now, now))
		mock.ExpectQuery("SELECT EXISTS").
			WithArgs(int32(1), int32(2)).
			WillReturnRows(sqlmock.NewRows([]string{"exists"}).AddRow(true))

		responder := AddSatelliteToGroup(groups.AddSatelliteToGroupParams{
			HTTPRequest: handlerRequest(http.MethodPost, "/api/groups/satellite"),
			Body:        &swaggermodels.SatelliteGroupParams{Satellite: "edge-01", Group: "edge"},
		}, handlerTestPrincipal)

		response, ok := responder.(*groups.AddSatelliteToGroupOK)
		require.True(t, ok)
		require.Equal(t, "Satellite is already in the group", response.Payload.Message)
	})
}

func TestGroupMutationHandlersRejectMissingBodies(t *testing.T) {
	newMockHandlerService(t)

	_, removeBadRequest := RemoveSatelliteFromGroup(groups.RemoveSatelliteFromGroupParams{
		HTTPRequest: handlerRequest(http.MethodDelete, "/api/groups/satellite"),
	}, handlerTestPrincipal).(*groups.RemoveSatelliteFromGroupBadRequest)
	require.True(t, removeBadRequest)

	_, syncBadRequest := SyncGroup(groups.SyncGroupParams{
		HTTPRequest: handlerRequest(http.MethodPost, "/api/groups/sync"),
	}, handlerTestPrincipal).(*groups.SyncGroupBadRequest)
	require.True(t, syncBadRequest)

	_, invalidGroup := SyncGroup(groups.SyncGroupParams{
		HTTPRequest: handlerRequest(http.MethodPost, "/api/groups/sync"),
		Body:        &swaggermodels.StateArtifact{Group: "Invalid Group"},
	}, handlerTestPrincipal).(*groups.SyncGroupBadRequest)
	require.True(t, invalidGroup)
}

func TestSyncGroupReportsFailureStage(t *testing.T) {
	mock := newMockHandlerService(t)
	mock.ExpectBegin()
	mock.ExpectQuery("INSERT INTO groups").WillReturnError(errors.New("database unavailable"))
	mock.ExpectRollback()

	responder := SyncGroup(groups.SyncGroupParams{
		HTTPRequest: handlerRequest(http.MethodPost, "/api/groups/sync"),
		Body: &swaggermodels.StateArtifact{
			Group: "sg1",
			Artifacts: []*swaggermodels.Artifact{
				{Repository: "oci-proxy/busybox", Tag: []string{"latest"}},
			},
		},
	}, handlerTestPrincipal)

	response, ok := responder.(*groups.SyncGroupInternalServerError)
	require.True(t, ok)
	require.Equal(t, "Failed to create or update group record", response.Payload.Message)
}
