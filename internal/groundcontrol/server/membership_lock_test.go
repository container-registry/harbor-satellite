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
	"github.com/container-registry/harbor-satellite/internal/groundcontrol/spiffe"
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

// Both ZTR flows push satellite state built from the satellite's groups, so
// they must read those groups after taking the membership lock. Otherwise a
// membership change that commits mid-ZTR is overwritten by stale state.
// Failing the group read right after the lock keeps Harbor out of the test.
func TestZtrHandlersReadMembershipUnderLock(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	satelliteRow := func() *sqlmock.Rows {
		return sqlmock.NewRows([]string{"id", "name", "created_at", "updated_at", "last_seen", "heartbeat_interval"}).
			AddRow(7, "sat-1", now, now, sql.NullTime{}, sql.NullString{})
	}
	robotRow := func() *sqlmock.Rows {
		return sqlmock.NewRows([]string{"id", "robot_name", "robot_secret_hash", "robot_id", "satellite_id", "robot_expiry", "created_at", "updated_at"}).
			AddRow(3, "robot$satellite-sat-1", "hash", "42", 7, sql.NullTime{}, now, now)
	}

	tests := []struct {
		name   string
		expect func(mock sqlmock.Sqlmock)
		call   func(t *testing.T, s *Server, w http.ResponseWriter)
	}{
		{
			name: "Ztr",
			expect: func(mock sqlmock.Sqlmock) {
				mock.ExpectQuery("FROM satellite_token").WithArgs("ztr-token").
					WillReturnRows(sqlmock.NewRows([]string{"id", "satellite_id", "token", "created_at", "updated_at", "expires_at"}).
						AddRow(1, 7, "ztr-token", now, now, now.Add(time.Hour)))
				mock.ExpectQuery("FROM robot_accounts").WithArgs(int32(7)).WillReturnRows(robotRow())
				mock.ExpectQuery("FROM satellites").WithArgs(int32(7)).WillReturnRows(satelliteRow())
			},
			call: func(t *testing.T, s *Server, w http.ResponseWriter) {
				body, err := json.Marshal(ZTRRequest{Token: "ztr-token"})
				require.NoError(t, err)
				s.Ztr(w, httptest.NewRequest(http.MethodPost, "/satellites/ztr", bytes.NewReader(body)))
			},
		},
		{
			name: "SpiffeZtr",
			expect: func(mock sqlmock.Sqlmock) {
				mock.ExpectQuery("FROM satellites").WithArgs("sat-1").WillReturnRows(satelliteRow())
				mock.ExpectQuery("FROM robot_accounts").WithArgs(int32(7)).WillReturnRows(robotRow())
			},
			call: func(_ *testing.T, s *Server, w http.ResponseWriter) {
				req := httptest.NewRequest(http.MethodGet, "/satellites/spiffe-ztr", nil)
				req = req.WithContext(spiffe.ContextWithSatelliteName(req.Context(), "sat-1"))
				s.SpiffeZtr(w, req)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("SKIP_HARBOR_HEALTH_CHECK", "true")
			loadGCForTest(t)
			server, mock := newMockServer(t)

			tt.expect(mock)
			mock.ExpectBegin()
			mock.ExpectExec("pg_advisory_xact_lock").WithArgs(membershipLockID).WillReturnResult(sqlmock.NewResult(0, 1))
			mock.ExpectQuery("FROM satellite_groups").WithArgs(int32(7)).
				WillReturnError(errors.New("connection reset"))
			mock.ExpectRollback()

			rr := httptest.NewRecorder()
			tt.call(t, server, rr)

			require.Equal(t, http.StatusInternalServerError, rr.Code)
			require.NoError(t, mock.ExpectationsWereMet())
		})
	}
}
