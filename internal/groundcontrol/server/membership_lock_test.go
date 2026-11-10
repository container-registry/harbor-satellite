package server

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/lib/pq"
	"github.com/stretchr/testify/require"
)

// Each handler that writes Harbor state derived from group membership must
// take the membership lock before any other statement in its transaction.
// sqlmock checks order, so a failed lock that is followed by anything but a
// rollback fails the test. RegisterSatellite is missing because it asks the
// Harbor client for the robot before it opens the transaction.
func TestGroupMembershipHandlersLockFirst(t *testing.T) {
	membershipBody := func(t *testing.T) []byte {
		t.Helper()
		body, err := json.Marshal(SatelliteGroupRequest{Group: "edge-a", Satellite: "sat-1"})
		require.NoError(t, err)
		return body
	}

	tests := []struct {
		name string
		call func(t *testing.T, s *Server, w http.ResponseWriter)
	}{
		{
			name: "SyncGroup",
			call: func(t *testing.T, s *Server, w http.ResponseWriter) {
				body, err := json.Marshal(GroupSyncRequest{Group: "edge-a"})
				require.NoError(t, err)
				s.SyncGroup(w, httptest.NewRequest(http.MethodPost, "/api/groups/sync", bytes.NewReader(body)))
			},
		},
		{
			name: "DeleteGroup",
			call: func(_ *testing.T, s *Server, w http.ResponseWriter) {
				s.DeleteGroup(w, httptest.NewRequest(http.MethodDelete, "/api/groups/edge-a", nil), "edge-a")
			},
		},
		{
			name: "AddSatelliteToGroup",
			call: func(t *testing.T, s *Server, w http.ResponseWriter) {
				s.AddSatelliteToGroup(w, httptest.NewRequest(http.MethodPost, "/api/groups/satellite", bytes.NewReader(membershipBody(t))))
			},
		},
		{
			name: "SetSatelliteConfig",
			call: func(t *testing.T, s *Server, w http.ResponseWriter) {
				body, err := json.Marshal(SatelliteConfigRequest{Satellite: "sat-1", ConfigName: "default"})
				require.NoError(t, err)
				s.SetSatelliteConfig(w, httptest.NewRequest(http.MethodPost, "/api/configs/satellite", bytes.NewReader(body)))
			},
		},
		{
			name: "RemoveSatelliteFromGroup",
			call: func(t *testing.T, s *Server, w http.ResponseWriter) {
				s.RemoveSatelliteFromGroup(w, httptest.NewRequest(http.MethodDelete, "/api/groups/satellite", bytes.NewReader(membershipBody(t))))
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server, mock := newMockServer(t)
			server.groupSync = &fakeGroupSync{}

			mock.ExpectBegin()
			mock.ExpectExec("pg_advisory_xact_lock").WithArgs(membershipLockID).
				WillReturnError(errors.New("lock unavailable"))
			mock.ExpectRollback()

			rr := httptest.NewRecorder()
			tt.call(t, server, rr)

			require.Equal(t, http.StatusInternalServerError, rr.Code)
			require.NoError(t, mock.ExpectationsWereMet())
		})
	}
}

// A satellite already in the group is detected under the lock, so a removal
// that commits just before the add can't make the add look like a no-op.
func TestAddSatelliteToGroupChecksMembershipUnderLock(t *testing.T) {
	server, mock := newMockServer(t)
	now := time.Now().UTC().Truncate(time.Second)

	mock.ExpectBegin()
	mock.ExpectExec("pg_advisory_xact_lock").WithArgs(membershipLockID).WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectQuery("FROM satellites").WithArgs("sat-1").
		WillReturnRows(sqlmock.NewRows([]string{"id", "name", "created_at", "updated_at", "last_seen", "heartbeat_interval"}).
			AddRow(7, "sat-1", now, now, sql.NullTime{}, sql.NullString{}))
	mock.ExpectQuery("FROM groups").WithArgs("edge-a").
		WillReturnRows(sqlmock.NewRows([]string{"id", "group_name", "registry_url", "projects", "created_at", "updated_at"}).
			AddRow(1, "edge-a", "http://harbor:8080", pq.Array([]string{"store-a"}), now, now))
	mock.ExpectQuery("FROM satellite_groups").WithArgs(int32(7), int32(1)).
		WillReturnRows(sqlmock.NewRows([]string{"exists"}).AddRow(true))
	mock.ExpectRollback()

	body, err := json.Marshal(SatelliteGroupRequest{Group: "edge-a", Satellite: "sat-1"})
	require.NoError(t, err)
	rr := httptest.NewRecorder()
	server.AddSatelliteToGroup(rr, httptest.NewRequest(http.MethodPost, "/api/groups/satellite", bytes.NewReader(body)))

	require.Equal(t, http.StatusOK, rr.Code)
	require.Contains(t, rr.Body.String(), "already in the group")
	require.NoError(t, mock.ExpectationsWereMet())
}
