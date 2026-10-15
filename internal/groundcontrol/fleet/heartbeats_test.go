package fleet_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/container-registry/harbor-satellite/internal/groundcontrol/fleet"
	"github.com/container-registry/harbor-satellite/internal/shared/inventory"
	"github.com/container-registry/harbor-satellite/internal/shared/testutil/pending"
	"github.com/stretchr/testify/require"
)

const pendingHeartbeat = "the heartbeat writes 8 statements in the request; there is no buffer"

func sessionAuth() fleet.SatelliteAuth {
	return fleet.SatelliteAuth{
		Method: fleet.AuthSession, SessionID: 11, Satellite: 7, SatelliteName: "edge-01",
		ExpiresAt: now.Add(600 * time.Hour), State: fleet.StateOnline, RobotExpiresAt: now.Add(30 * 24 * time.Hour), HasSession: true,
	}
}

func plainBeat(auth fleet.SatelliteAuth) fleet.HeartbeatCmd {
	return fleet.HeartbeatCmd{
		Auth:     auth,
		Interval: "@every 00h00m30s",
		Sample:   fleet.StatusSample{Satellite: auth.Satellite, Activity: "idle", CPUPercent: 1.5, ReportedAt: now},
	}
}

func beat(t *testing.T, w *world, cmd fleet.HeartbeatCmd) (fleet.HeartbeatResult, error) {
	t.Helper()
	return w.services(t).Heartbeats.Beat(context.Background(), cmd)
}

func TestHeartbeatService_PlainBeatTouchesNoRepository(t *testing.T) {
	pending.Plan(t, "4.2", pendingHeartbeat)
	w := newWorld()

	res, err := beat(t, w, plainBeat(sessionAuth()))

	require.NoError(t, err)
	require.Empty(t, res.Events)
	require.Nil(t, res.Journal)
	require.Nil(t, res.Session)
	for _, m := range w.rec.Methods() {
		require.True(t, strings.HasPrefix(m, "Heartbeats.") || m == "Sessions.Renew", "a plain beat called %s: %v", m, w.rec.Methods())
	}
	beats := w.beats.Beats()
	require.Len(t, beats, 1)
	require.Equal(t, fleet.SatelliteID(7), beats[0].Satellite)
	require.Equal(t, now, beats[0].SeenAt)
	require.Equal(t, "@every 00h00m30s", beats[0].Interval)
}

func TestHeartbeatService_RejectsOversizedFields(t *testing.T) {
	long := func(n int) string { return strings.Repeat("a", n) }
	for name, change := range map[string]func(c *fleet.HeartbeatCmd){
		"activity over 255 bytes":      func(c *fleet.HeartbeatCmd) { c.Sample.Activity = long(256) },
		"state digest over 255 bytes":  func(c *fleet.HeartbeatCmd) { c.StateDigest = fleet.Digest(long(256)) },
		"config digest over 255 bytes": func(c *fleet.HeartbeatCmd) { c.ConfigDigest = fleet.Digest(long(256)) },
		"a reference over 512 bytes":   func(c *fleet.HeartbeatCmd) { c.Inventory = []fleet.InventoryItem{{Reference: long(513)}} },
		"cpu above 999.99":             func(c *fleet.HeartbeatCmd) { c.Sample.CPUPercent = 1000 },
		"negative cpu":                 func(c *fleet.HeartbeatCmd) { c.Sample.CPUPercent = -1 },
		"negative memory":              func(c *fleet.HeartbeatCmd) { c.Sample.MemoryUsedBytes = -1 },
		"negative image count":         func(c *fleet.HeartbeatCmd) { c.Sample.ImageCount = -1 },
		"an applied state digest too long": func(c *fleet.HeartbeatCmd) {
			c.AppliedStates = []fleet.AppliedState{{Ref: "g1", Digest: fleet.Digest(long(256))}}
		},
	} {
		t.Run(name, func(t *testing.T) {
			pending.Plan(t, "4.2", pendingHeartbeat)
			w := newWorld()
			cmd := plainBeat(sessionAuth())
			change(&cmd)

			_, err := beat(t, w, cmd)

			require.ErrorIs(t, err, fleet.ErrInvalid)
			require.Empty(t, w.beats.Beats(), "nothing is recorded")
		})
	}
}

func TestHeartbeatService_RefreshCredentialsFromCachedExpiry(t *testing.T) {
	for name, tc := range map[string]struct {
		expires time.Time
		want    []string
	}{
		"expires within two intervals": {expires: now.Add(50 * time.Second), want: []string{"refresh_credentials"}},
		"expires later":                {expires: now.Add(time.Hour)},
		"no expiry":                    {},
	} {
		t.Run(name, func(t *testing.T) {
			pending.Plan(t, "4.2", pendingHeartbeat)
			w := newWorld()
			auth := sessionAuth()
			auth.RobotExpiresAt = tc.expires

			res, err := beat(t, w, plainBeat(auth))

			require.NoError(t, err)
			require.Equal(t, tc.want, res.Events)
			require.Zero(t, w.rec.Count("Robots.BySatellite"), "the expiry comes from the cached auth")
		})
	}
}

func TestHeartbeatService_RevokedIsErrRevoked(t *testing.T) {
	pending.Plan(t, "4.3", pendingLifecycle)
	w := newWorld()
	auth := sessionAuth()
	auth.State = fleet.StateRevoked

	_, err := beat(t, w, plainBeat(auth))

	require.ErrorIs(t, err, fleet.ErrRevoked)
	require.Empty(t, w.beats.Beats())
}

func TestHeartbeatService_RenewsSessionInLastThird(t *testing.T) {
	for name, renewed := range map[string]bool{"in the last third": true, "earlier": false} {
		t.Run(name, func(t *testing.T) {
			pending.Plan(t, "4.1", pendingSessions)
			w := newWorld()
			w.sessions.Renewed = renewed
			w.sessions.Grant = fleet.SessionGrant{Token: "hss_next", ExpiresAt: now.Add(720 * time.Hour)}

			res, err := beat(t, w, plainBeat(sessionAuth()))

			require.NoError(t, err)
			require.Equal(t, sessionAuth(), arg[fleet.SatelliteAuth](t, onlyCall(t, w.rec, "Sessions.Renew"), 0))
			if renewed {
				require.NotNil(t, res.Session)
				require.Equal(t, w.sessions.Grant, *res.Session)
			} else {
				require.Nil(t, res.Session)
			}
		})
	}

	t.Run("an SVID beat never renews", func(t *testing.T) {
		pending.Plan(t, "4.1", pendingSessions)
		w := newWorld()
		auth := sessionAuth()
		auth.Method, auth.SessionID, auth.HasSession = fleet.AuthSVID, 0, false

		_, err := beat(t, w, plainBeat(auth))

		require.NoError(t, err)
		require.Zero(t, w.rec.Count("Sessions.Renew"))
		require.Zero(t, w.rec.Count("Sessions.IssueIfNone"))
	})
}

func TestHeartbeatService_RobotBeatWithoutSessionGetsOneOnce(t *testing.T) {
	robot := sessionAuth()
	robot.Method, robot.SessionID, robot.HasSession = fleet.AuthRobot, 0, false

	t.Run("without a session", func(t *testing.T) {
		pending.Plan(t, "4.1", pendingSessions)
		w := newWorld()
		w.sessions.IssuedIfNone = true
		w.sessions.Grant = fleet.SessionGrant{Token: "hss_first", ExpiresAt: now.Add(720 * time.Hour)}

		res, err := beat(t, w, plainBeat(robot))

		require.NoError(t, err)
		require.Equal(t, fleet.SatelliteID(7), arg[fleet.SatelliteID](t, onlyCall(t, w.rec, "Sessions.IssueIfNone"), 0))
		require.NotNil(t, res.Session)
		require.Equal(t, "hss_first", res.Session.Token)
	})

	t.Run("another replica issued it first", func(t *testing.T) {
		pending.Plan(t, "4.1", pendingSessions)
		w := newWorld()

		res, err := beat(t, w, plainBeat(robot))

		require.NoError(t, err)
		require.Equal(t, 1, w.rec.Count("Sessions.IssueIfNone"))
		require.Nil(t, res.Session)
	})

	t.Run("with a session", func(t *testing.T) {
		pending.Plan(t, "4.1", pendingSessions)
		w := newWorld()
		withSession := robot
		withSession.HasSession = true

		_, err := beat(t, w, plainBeat(withSession))

		require.NoError(t, err)
		require.Zero(t, w.rec.Count("Sessions.IssueIfNone"))
		require.Zero(t, w.rec.Count("Sessions.Renew"), "a robot beat has no session to renew")
	})
}

func TestHeartbeatService_JournalForwardsUploadAndReturnsAck(t *testing.T) {
	pending.Plan(t, "6.5", pendingJournal)
	w := newWorld()
	w.repo.Journal.Cursor = fleet.JournalCursor{Epoch: journalEpoch}
	cmd := plainBeat(sessionAuth())
	cmd.Journal = &fleet.JournalUpload{Epoch: journalEpoch, Records: replicated(t, 1, 2)}

	res, err := beat(t, w, cmd)

	require.NoError(t, err)
	require.NotNil(t, res.Journal)
	require.Equal(t, fleet.JournalAck{Epoch: journalEpoch, AckedSeq: 2}, *res.Journal)
	require.Equal(t, []any{fleet.SatelliteID(7), journalEpoch}, onlyCall(t, w.rec, "Journal.OpenCursor").Args)
	before(t, w.rec, "Heartbeats.Record", "Journal.OpenCursor")
}

func TestHeartbeatService_MalformedJournalRecordsBeatAndReturnsAckError(t *testing.T) {
	pending.Plan(t, "6.5", pendingJournal)
	w := newWorld()
	cmd := plainBeat(sessionAuth())
	cmd.Journal = &fleet.JournalUpload{Epoch: 0, Records: replicated(t, 1)}

	res, err := beat(t, w, cmd)

	require.NoError(t, err, "a malformed journal never fails the heartbeat")
	require.Len(t, w.beats.Beats(), 1)
	require.NotNil(t, res.Journal)
	require.NotEmpty(t, res.Journal.Error)
	require.Zero(t, w.rec.Count("Journal.Advance"))
}

func TestHeartbeatService_RequestsInventoryOnDigestChange(t *testing.T) {
	items := []fleet.InventoryItem{{Reference: "library/alpine:3.20", SizeBytes: 3 << 20}}
	known := fleet.Digest(inventory.Digest(items))
	for name, tc := range map[string]struct {
		reported  fleet.Digest
		inventory []fleet.InventoryItem
		want      bool
	}{
		"unchanged digest":         {reported: known},
		"changed digest, no list":  {reported: "sha256:other", want: true},
		"changed digest with list": {reported: fleet.Digest(inventory.Digest(nil)), inventory: []fleet.InventoryItem{}},
	} {
		t.Run(name, func(t *testing.T) {
			pending.Plan(t, "4.4", "every heartbeat rewrites the cached images")
			w := newWorld()
			w.beats.Digests = map[fleet.SatelliteID]fleet.Digest{7: known}
			cmd := plainBeat(sessionAuth())
			cmd.InventoryDigest, cmd.Inventory = tc.reported, tc.inventory

			res, err := beat(t, w, cmd)

			require.NoError(t, err)
			require.Equal(t, tc.want, res.InventoryRequested)
		})
	}
}

func TestHeartbeatService_ComputesDigestForLegacyList(t *testing.T) {
	pending.Plan(t, "4.4", "every heartbeat rewrites the cached images")
	w := newWorld()
	items := []fleet.InventoryItem{{Reference: "library/busybox:1.36", SizeBytes: 1 << 20}, {Reference: "library/alpine:3.20", SizeBytes: 3 << 20}}
	cmd := plainBeat(sessionAuth())
	cmd.Inventory = items

	res, err := beat(t, w, cmd)

	require.NoError(t, err)
	require.False(t, res.InventoryRequested)
	beats := w.beats.Beats()
	require.Len(t, beats, 1)
	require.Equal(t, fleet.Digest(inventory.Digest(items)), beats[0].InventoryDigest)
	require.Equal(t, items, beats[0].Inventory)
}
