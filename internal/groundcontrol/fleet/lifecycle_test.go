package fleet_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/container-registry/harbor-satellite/internal/groundcontrol/fleet"
	"github.com/container-registry/harbor-satellite/internal/shared/testutil/pending"
	"github.com/stretchr/testify/require"
)

const (
	pendingSessions  = "Ground Control issues no satellite sessions; every beat verifies the robot secret"
	pendingLifecycle = "ADR-0006 lifecycle is not implemented; satellites are never revoked or stale"
)

var errBoom = errors.New("boom")

func TestSatelliteView_Converged(t *testing.T) {
	converged := fleet.SatelliteView{
		AppliedStateDigest:      "sha256:sat",
		PublishedArtifactDigest: "sha256:sat",
		AppliedStates:           []fleet.AppliedState{{Ref: "g1-ref", Digest: "sha256:g1"}, {Ref: "g2-ref", Digest: "sha256:g2"}},
		PublishedGroups: []fleet.PublishedGroup{
			{Group: "g1", Ref: "g1-ref", ArtifactDigest: "sha256:g1"},
			{Group: "g2", Ref: "g2-ref", ArtifactDigest: "sha256:g2"},
		},
	}
	for name, tc := range map[string]struct {
		change func(v *fleet.SatelliteView)
		want   bool
	}{
		"converged":                         {change: func(*fleet.SatelliteView) {}, want: true},
		"a group missing from applied":      {change: func(v *fleet.SatelliteView) { v.AppliedStates = v.AppliedStates[:1] }},
		"a stale group digest":              {change: func(v *fleet.SatelliteView) { v.AppliedStates[1].Digest = "sha256:old" }},
		"a group published but not applied": {change: func(v *fleet.SatelliteView) { v.AppliedStates = nil }},
		"a group not yet published":         {change: func(v *fleet.SatelliteView) { v.PublishedGroups[1].ArtifactDigest = "" }},
		"a satellite digest that differs":   {change: func(v *fleet.SatelliteView) { v.AppliedStateDigest = "sha256:other" }},
		"nothing applied yet":               {change: func(v *fleet.SatelliteView) { v.AppliedStateDigest = "" }},
	} {
		t.Run(name, func(t *testing.T) {
			v := converged
			v.AppliedStates = append([]fleet.AppliedState(nil), converged.AppliedStates...)
			v.PublishedGroups = append([]fleet.PublishedGroup(nil), converged.PublishedGroups...)
			tc.change(&v)
			require.Equal(t, tc.want, v.Converged())
		})
	}
}

func seedView(w *world, state fleet.LifecycleState) {
	w.repo.Views.ByID = map[fleet.SatelliteID]fleet.SatelliteView{7: {Satellite: fleet.Satellite{ID: 7, Name: "edge-01"}, State: state}}
}

func TestRegistrationService_ZeroTouchIssuesSessionBeforeTx(t *testing.T) {
	t.Run("rotates, issues, then commits", func(t *testing.T) {
		pending.Plan(t, "4.1", pendingSessions)
		w := newWorld()
		seedZTR(w)
		seedView(w, fleet.StatePending)
		w.sessions.Grant = fleet.SessionGrant{Token: "hss_new", ExpiresAt: now.Add(720 * 3600e9)}

		res, err := w.services(t).Registration.ZeroTouch(context.Background(), "tok")

		require.NoError(t, err)
		require.NotNil(t, res.Session)
		require.Equal(t, w.sessions.Grant, *res.Session)
		require.Equal(t, fleet.SatelliteID(7), arg[fleet.SatelliteID](t, onlyCall(t, w.rec, "Sessions.Issue"), 0))
		before(t, w.rec, "RotateRobotSecret", "Sessions.Issue")
		before(t, w.rec, "Sessions.Issue", "WithTx")
		outsideTx(t, w.rec, "Sessions.Issue")
	})

	t.Run("a failed commit unclaims and invalidates the session", func(t *testing.T) {
		pending.Plan(t, "4.1", pendingSessions)
		w := newWorld()
		seedZTR(w)
		seedView(w, fleet.StatePending)
		w.rec.FailOn("Commit", errBoom)

		_, err := w.services(t).Registration.ZeroTouch(context.Background(), "tok")

		require.ErrorIs(t, err, errBoom)
		require.Equal(t, 1, w.rec.Count("Tokens.Unclaim"))
		require.Equal(t, fleet.SatelliteID(7), arg[fleet.SatelliteID](t, onlyCall(t, w.rec, "Sessions.Invalidate"), 0))
	})
}

func TestRegistrationService_ZeroTouchChecksRevokedRightAfterClaim(t *testing.T) {
	pending.Plan(t, "4.3", pendingLifecycle)
	w := newWorld()
	seedZTR(w)
	seedView(w, fleet.StateRevoked)

	_, err := w.services(t).Registration.ZeroTouch(context.Background(), "tok")

	require.ErrorIs(t, err, fleet.ErrRevoked)
	require.Equal(t, []string{"Tokens.Claim", "Views.Get", "Tokens.Unclaim"}, w.rec.Methods())
	noRegistryCalls(t, w.rec)
}

func TestRegistrationService_SPIFFEZeroTouchRefusesRevoked(t *testing.T) {
	pending.Plan(t, "4.3", pendingLifecycle)
	w := newWorld()
	seedZTR(w)
	seedView(w, fleet.StateRevoked)

	_, err := w.services(t).Registration.SPIFFEZeroTouch(context.Background(), "edge-01")

	require.ErrorIs(t, err, fleet.ErrRevoked)
	noRegistryCalls(t, w.rec)
	require.Zero(t, w.rec.Count("Satellites.Create"))
	require.Zero(t, w.rec.Count("Sessions.Issue"))
}

func TestRegistrationService_SPIFFEZeroTouchStoresIdentity(t *testing.T) {
	pending.Plan(t, "4.3", pendingLifecycle)
	w := newWorld()
	seedZTR(w)
	seedView(w, fleet.StateOnline)
	w.identity.Domain = "harbor-satellite.local"

	res, err := w.services(t).Registration.SPIFFEZeroTouch(context.Background(), "edge-01")

	require.NoError(t, err)
	require.Nil(t, res.Session, "the SVID authenticates every beat")
	require.Zero(t, w.rec.Count("Sessions.Issue"))
	set := onlyCall(t, w.rec, "Satellites.SetIdentity")
	require.Equal(t, fleet.SatelliteID(7), arg[fleet.SatelliteID](t, set, 0))
	spiffeID := arg[string](t, set, 1)
	require.True(t, strings.HasPrefix(spiffeID, "spiffe://harbor-satellite.local/"), spiffeID)
	require.True(t, strings.HasSuffix(spiffeID, "/edge-01"), spiffeID)
}

func TestSatelliteService_DeleteTombstoneCarriesSPIFFEID(t *testing.T) {
	spiffeID := "spiffe://harbor-satellite.local/satellite/region/default/edge-01"
	for name, tc := range map[string]struct{ spiffeID string }{
		"registered through SPIRE": {spiffeID: spiffeID},
		"token satellite":          {},
	} {
		t.Run(name, func(t *testing.T) {
			pending.Plan(t, "4.3", "satellite_identities is not read; the tombstone has no SPIFFE ID")
			w := newWorld()
			w.repo.Satellites.ByName = map[string]fleet.Satellite{"edge-01": {ID: 7, Name: "edge-01", SPIFFEID: tc.spiffeID}}
			w.repo.Robots.BySatelliteID = map[fleet.SatelliteID]fleet.Robot{7: {ID: 5, Satellite: 7, HarborID: 42}}

			require.NoError(t, w.services(t).Satellites.Delete(context.Background(), "edge-01"))

			events := w.repo.Events.Appended()
			require.Len(t, events, 1)
			payload, ok := events[0].Payload.(fleet.SatelliteDeletedPayload)
			require.True(t, ok, "%T", events[0].Payload)
			require.Equal(t, tc.spiffeID, payload.SPIFFEID)
		})
	}
}

func TestSatelliteService_DeleteInvalidatesSessions(t *testing.T) {
	pending.Plan(t, "4.1", pendingSessions)
	w := newWorld()
	w.repo.Satellites.ByName = map[string]fleet.Satellite{"edge-01": {ID: 7, Name: "edge-01"}}
	w.repo.Robots.BySatelliteID = map[fleet.SatelliteID]fleet.Robot{7: {ID: 5, Satellite: 7, HarborID: 42}}

	require.NoError(t, w.services(t).Satellites.Delete(context.Background(), "edge-01"))

	require.Equal(t, fleet.SatelliteID(7), arg[fleet.SatelliteID](t, onlyCall(t, w.rec, "Sessions.Invalidate"), 0))
	before(t, w.rec, "Commit", "Sessions.Invalidate")
}

func lifecycleEvents(w *world, kind fleet.EventKind) []fleet.Event {
	var out []fleet.Event
	for _, e := range w.repo.Events.Appended() {
		if e.Kind == kind {
			out = append(out, e)
		}
	}
	return out
}

func TestSatelliteService_RevokeAppendsEventOnce(t *testing.T) {
	t.Run("an online satellite", func(t *testing.T) {
		pending.Plan(t, "4.3", pendingLifecycle)
		w := newWorld()
		w.repo.Satellites.ByName = map[string]fleet.Satellite{"edge-01": {ID: 7, Name: "edge-01"}}
		seedView(w, fleet.StateRevoked)
		w.repo.Views.Previous = fleet.StateOnline

		view, err := w.services(t).Satellites.Revoke(context.Background(), "edge-01")

		require.NoError(t, err)
		require.Equal(t, fleet.StateRevoked, view.State)
		insideTx(t, w.rec, "Views.Revoke")
		events := lifecycleEvents(w, fleet.EventSatelliteRevoked)
		require.Len(t, events, 1)
		require.Equal(t, fleet.Aggregate{Type: fleet.AggregateSatellite, ID: 7}, events[0].Aggregate)
		payload, ok := events[0].Payload.(fleet.SatelliteLifecyclePayload)
		require.True(t, ok, "%T", events[0].Payload)
		require.Equal(t, fleet.SatelliteLifecyclePayload{Name: "edge-01", State: fleet.StateRevoked, PreviousState: fleet.StateOnline}, fleet.SatelliteLifecyclePayload{Name: payload.Name, State: payload.State, PreviousState: payload.PreviousState})
		require.Zero(t, w.rec.Count("Sessions.Invalidate"), "a revoked satellite keeps its sessions")
	})

	t.Run("an already revoked satellite", func(t *testing.T) {
		pending.Plan(t, "4.3", pendingLifecycle)
		w := newWorld()
		w.repo.Satellites.ByName = map[string]fleet.Satellite{"edge-01": {ID: 7, Name: "edge-01"}}
		seedView(w, fleet.StateRevoked)
		w.repo.Views.Previous = fleet.StateRevoked

		_, err := w.services(t).Satellites.Revoke(context.Background(), "edge-01")

		require.NoError(t, err)
		require.Empty(t, lifecycleEvents(w, fleet.EventSatelliteRevoked))
	})
}

func TestSatelliteService_UnrevokeReturnsToPending(t *testing.T) {
	for name, changed := range map[string]bool{"a revoked satellite": true, "a satellite that is not revoked": false} {
		t.Run(name, func(t *testing.T) {
			pending.Plan(t, "4.3", pendingLifecycle)
			w := newWorld()
			w.repo.Satellites.ByName = map[string]fleet.Satellite{"edge-01": {ID: 7, Name: "edge-01"}}
			seedView(w, fleet.StatePending)
			w.repo.Views.Changed = changed

			view, err := w.services(t).Satellites.Unrevoke(context.Background(), "edge-01")

			require.NoError(t, err)
			require.Equal(t, fleet.StatePending, view.State)
			want := 0
			if changed {
				want = 1
			}
			require.Len(t, lifecycleEvents(w, fleet.EventSatelliteUnrevoked), want)
		})
	}
}

func TestSatelliteService_ViewFillsPublishedRefs(t *testing.T) {
	pending.Plan(t, "4.3", pendingLifecycle)
	w := newWorld()
	w.repo.Views.ByID = map[fleet.SatelliteID]fleet.SatelliteView{7: {
		Satellite:       fleet.Satellite{ID: 7, Name: "edge-01"},
		State:           fleet.StateOnline,
		PublishedGroups: []fleet.PublishedGroup{{Group: "g1", ArtifactDigest: "sha256:g1"}},
	}}

	view, err := w.services(t).Satellites.View(context.Background(), "edge-01")

	require.NoError(t, err)
	require.Len(t, view.PublishedGroups, 1)
	require.Equal(t, stateRef("group", "g1").String(), view.PublishedGroups[0].Ref)
}
