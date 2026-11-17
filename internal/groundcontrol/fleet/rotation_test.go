package fleet_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/container-registry/harbor-satellite/internal/groundcontrol/fleet"
	"github.com/container-registry/harbor-satellite/internal/groundcontrol/fleet/fleettest"
	"github.com/stretchr/testify/require"
)

func TestSuccessorRobotName(t *testing.T) {
	require.Equal(t, "edge-01-7-g1", fleet.SuccessorRobotName(7, "edge-01", 1))
	require.Equal(t, "edge-01-7-g12", fleet.SuccessorRobotName(7, "edge-01", 12))
	require.NotEqual(t, fleet.SuccessorRobotName(7, "edge-01", 2), fleet.SuccessorRobotName(8, "edge-01", 2), "unique per satellite ID")
	require.NotEqual(t, fleet.RobotName(7, "edge-01"), fleet.SuccessorRobotName(7, "edge-01", 1))

	long := strings.Repeat("a", 255)
	name := fleet.SuccessorRobotName(7, long, 12)
	require.LessOrEqual(t, len("robot$"+name), 255, "fits robot_accounts.robot_name")
	require.True(t, strings.HasSuffix(name, "-7-g12"), name)
	require.NotEqual(t, name, fleet.SuccessorRobotName(8, long, 12))
}

// rotatingServices are the world's services with a rotation pending for
// satellite 7 at generation 2.
func (w *world) rotatingServices(t *testing.T) *fleet.Services {
	t.Helper()
	rotations := fleettest.NewRotationSource(w.rec)
	rotations.ByID = map[fleet.SatelliteID]fleet.RotatedCredentials{7: {
		Username: "robot$edge-01-7-g2", Password: "successor", ExpiresAt: now.Add(30 * 24 * time.Hour), Generation: 2,
	}}
	svc, err := fleet.NewServices(fleet.Deps{
		Repository: w.repo, Registry: w.registry, Sessions: w.sessions, HeartbeatSink: w.beats, Rotations: rotations,
		Clock: fleettest.Clock{T: now}, Settings: fleet.Settings{RegistryURL: registryURL, HeartbeatFlushInterval: 5 * time.Second},
	})
	require.NoError(t, err)
	return svc
}

func TestHeartbeatService_DeliversPendingRotationToSessionAndSVIDOnly(t *testing.T) {
	for method, delivered := range map[fleet.AuthMethod]bool{fleet.AuthSession: true, fleet.AuthSVID: true, fleet.AuthRobot: false} {
		t.Run(string(method), func(t *testing.T) {
			w := newWorld()
			auth := sessionAuth()
			auth.Method = method
			cmd := plainBeat(auth)
			cmd.CredentialsGeneration = 1

			res, err := w.rotatingServices(t).Heartbeats.Beat(context.Background(), cmd)

			require.NoError(t, err)
			if !delivered {
				require.Nil(t, res.Credentials, "a robot Basic satellite would switch to a robot GC does not accept yet")
				return
			}
			require.NotNil(t, res.Credentials)
			require.Equal(t, int64(2), res.Credentials.Generation)
			require.Equal(t, "robot$edge-01-7-g2", res.Credentials.Username)
		})
	}
}

func TestHeartbeatService_NoDeliveryForCurrentGeneration(t *testing.T) {
	w := newWorld()
	cmd := plainBeat(sessionAuth())
	cmd.CredentialsGeneration = 2

	res, err := w.rotatingServices(t).Heartbeats.Beat(context.Background(), cmd)

	require.NoError(t, err)
	require.Nil(t, res.Credentials)
	beats := w.beats.Beats()
	require.Len(t, beats, 1)
	require.Equal(t, int64(2), beats[0].CredentialsGeneration, "the ack travels in the buffered beat")
}

// A satellite reports its generation on every beat; only an ack of the
// pending rotation reaches the buffer, so the flush pays AckRobotRotations
// only while one is due.
func TestHeartbeatService_ForwardsOnlyThePendingGeneration(t *testing.T) {
	w := newWorld()
	cmd := plainBeat(sessionAuth())
	cmd.CredentialsGeneration = 1

	res, err := w.rotatingServices(t).Heartbeats.Beat(context.Background(), cmd)

	require.NoError(t, err)
	require.NotNil(t, res.Credentials, "generation 1 is older than the pending 2")
	beats := w.beats.Beats()
	require.Len(t, beats, 1)
	require.Zero(t, beats[0].CredentialsGeneration, "not the pending generation, so no ack")
}

func TestSatelliteService_DeleteTombstoneIncludesUnpromotedSuccessor(t *testing.T) {
	w := newWorld()
	w.repo.Satellites.ByName = map[string]fleet.Satellite{"edge-01": {ID: 7, Name: "edge-01"}}
	w.repo.Robots.BySatelliteID = map[fleet.SatelliteID]fleet.Robot{7: {ID: 5, Satellite: 7, HarborID: 42}}
	w.repo.Robots.Successors = map[fleet.SatelliteID][]int64{7: {43}}

	require.NoError(t, w.services(t).Satellites.Delete(context.Background(), "edge-01"))

	events := w.repo.Events.Appended()
	require.Len(t, events, 1)
	payload, ok := events[0].Payload.(fleet.SatelliteDeletedPayload)
	require.True(t, ok, "%T", events[0].Payload)
	require.Equal(t, []int64{42, 43}, payload.RobotHarborIDs)
}

// A journal the repository cannot ingest never fails the beat: the
// renewed session, the pending rotation and the events still arrive, and
// the answer carries no journal object, so the satellite resends the batch
// instead of counting a rejection.
func TestHeartbeatService_JournalFailureKeepsTheRestOfTheBeat(t *testing.T) {
	for _, method := range []string{"Journal.OpenCursor", "Journal.Advance"} {
		t.Run(method, func(t *testing.T) {
			w := newWorld()
			w.repo.Journal.Cursor = fleet.JournalCursor{Epoch: journalEpoch}
			boom := errors.New("database down")
			w.rec.FailOn(method, boom)
			w.sessions.Renewed = true
			w.sessions.Grant = fleet.SessionGrant{Token: "hss_next", ExpiresAt: now.Add(720 * time.Hour)}
			auth := sessionAuth()
			auth.RobotExpiresAt = now.Add(50 * time.Second)
			cmd := plainBeat(auth)
			cmd.CredentialsGeneration = 1
			cmd.Journal = &fleet.JournalUpload{Epoch: journalEpoch, Records: replicated(t, 1, 2)}

			res, err := w.rotatingServices(t).Heartbeats.Beat(context.Background(), cmd)

			require.NoError(t, err)
			require.Nil(t, res.Journal, "no ack error, which the satellite would count as a rejection")
			require.ErrorIs(t, res.JournalErr, boom)
			require.Len(t, w.beats.Beats(), 1)
			require.NotNil(t, res.Session)
			require.Equal(t, "hss_next", res.Session.Token)
			require.NotNil(t, res.Credentials)
			require.Equal(t, []string{"refresh_credentials"}, res.Events)
		})
	}
}
