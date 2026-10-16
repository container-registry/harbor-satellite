package async_test

import (
	"context"
	"encoding/json"
	"errors"
	"math/rand/v2"
	"testing"
	"time"

	"github.com/container-registry/harbor-satellite/internal/groundcontrol/async"
	"github.com/container-registry/harbor-satellite/internal/groundcontrol/fleet"
	"github.com/container-registry/harbor-satellite/internal/groundcontrol/fleet/fleettest"
	"github.com/container-registry/harbor-satellite/internal/shared/testutil/pending"
	"github.com/stretchr/testify/require"
)

const (
	pendingReconcilers = "reconcilers are stubs; handlers call Harbor in the request"
	registryURL        = "http://harbor.example"
)

var settings = fleet.Settings{RegistryURL: registryURL, RobotDuration: 30 * 24 * time.Hour, TokenTTL: fleet.DefaultTokenTTL}

func ref(kind, name string) fleet.ArtifactRef {
	return fleet.ArtifactRef{Registry: registryURL, Repository: "satellite/" + kind + "-state/" + name + "/state", Tag: "latest"}
}

type plane struct {
	rec      *fleettest.Recorder
	repo     *fleettest.Repository
	registry *fleettest.Registry
	identity *fleettest.WorkloadIdentity
	desired  *fakeDesired
}

// newPlane is satellite edge-01 (7, robot 42) in groups g1 (library) and
// g2 (library, team) with config edge-config, and edge-02 (8, robot 43)
// in g1 only.
func newPlane() *plane {
	rec := fleettest.NewRecorder()
	p := &plane{rec: rec, repo: fleettest.NewRepository(rec), registry: fleettest.NewRegistry(rec), identity: fleettest.NewWorkloadIdentity(rec), desired: newFakeDesired()}
	g1 := fleet.Group{ID: 1, Name: "g1", Projects: []string{"library"}, Artifacts: []fleet.Artifact{{Repository: "library/alpine", Tags: []string{"3.20"}, Type: "image"}}}
	g2 := fleet.Group{ID: 2, Name: "g2", Projects: []string{"library", "team"}}
	cfg := fleet.Config{ID: 3, Name: "edge-config", Document: json.RawMessage(`{"app_config":{"log_level":"info"}}`)}
	p.repo.Satellites.ByName = map[string]fleet.Satellite{"edge-01": {ID: 7, Name: "edge-01"}, "edge-02": {ID: 8, Name: "edge-02"}}
	p.repo.Satellites.GroupsOf = map[fleet.SatelliteID][]fleet.Group{7: {g1, g2}, 8: {g1}}
	p.repo.Satellites.GrantsOf = map[fleet.SatelliteID]fleet.RobotGrant{
		7: {Satellite: 7, HarborID: 42, RobotName: "robot$edge-01", Projects: []string{"library", "library", "team"}},
		8: {Satellite: 8, HarborID: 43, RobotName: "robot$edge-02", Projects: []string{"library"}},
	}
	p.repo.Satellites.ConfigOf = map[fleet.SatelliteID]fleet.Config{7: cfg, 8: cfg}
	p.repo.Groups.ByName = map[string]fleet.Group{"g1": g1, "g2": g2}
	p.repo.Groups.MembersOf = map[fleet.GroupID][]fleet.SatelliteID{1: {7, 8}, 2: {7}}
	p.repo.Configs.ByName = map[string]fleet.Config{"edge-config": cfg}
	p.repo.Robots.BySatelliteID = map[fleet.SatelliteID]fleet.Robot{7: {ID: 5, Satellite: 7, HarborID: 42}, 8: {ID: 6, Satellite: 8, HarborID: 43}}
	p.registry.Digest = "sha256:pushed"
	return p
}

func (p *plane) satellites(t *testing.T) *async.SatelliteReconciler {
	t.Helper()
	r, err := async.NewSatelliteReconciler(p.repo, p.registry, p.desired, settings)
	require.NoError(t, err)
	return r
}

func (p *plane) tombstones(t *testing.T) *async.TombstoneHandler {
	t.Helper()
	h, err := async.NewTombstoneHandler(p.repo, p.registry, p.identity, p.desired)
	require.NoError(t, err)
	return h
}

func (p *plane) routes(t *testing.T) *async.Router {
	t.Helper()
	grp, err := async.NewGroupReconciler(p.repo, p.registry, p.desired, settings)
	require.NoError(t, err)
	cfg, err := async.NewConfigReconciler(p.repo, p.registry, p.desired, settings)
	require.NoError(t, err)
	router, err := async.NewRoutes(p.satellites(t), grp, cfg, p.tombstones(t))
	require.NoError(t, err)
	return router
}

func pushesTo(rec *fleettest.Recorder, target fleet.ArtifactRef) int {
	n := 0
	for _, c := range rec.CallsTo("PushState") {
		if c.Args[0] == target {
			n++
		}
	}
	return n
}

func event(t *testing.T, id int64, kind fleet.EventKind, typ fleet.AggregateType, aggregate int64, payload any) async.OutboxEvent {
	t.Helper()
	data, err := json.Marshal(payload)
	require.NoError(t, err)
	return async.OutboxEvent{ID: id, Kind: kind, Aggregate: fleet.Aggregate{Type: typ, ID: aggregate}, Payload: data}
}

func TestSatelliteReconciler_UnionAndSinglePush(t *testing.T) {
	pending.Plan(t, "2.3", pendingReconcilers)
	p := newPlane()
	r := p.satellites(t)

	require.NoError(t, r.Reconcile(context.Background(), 7))

	sets := p.rec.CallsTo("SetRobotAccess")
	require.Len(t, sets, 1)
	require.Equal(t, int64(42), sets[0].Args[0])
	require.Equal(t, []string{"library", fleet.SatelliteProject, "team"}, sets[0].Args[1], "fleet.RobotProjects of the groups")
	require.Equal(t, 1, pushesTo(p.rec, ref("satellite", "edge-01")))
	var doc struct {
		Config string   `json:"config"`
		States []string `json:"states"`
	}
	payload, ok := p.rec.CallsTo("PushState")[0].Args[1].([]byte)
	require.True(t, ok)
	require.NoError(t, json.Unmarshal(payload, &doc))
	require.Equal(t, []string{ref("group", "g1").String(), ref("group", "g2").String()}, doc.States)
	require.Equal(t, ref("config", "edge-config").String(), doc.Config)
	rec, err := p.desired.Get(context.Background(), fleet.Aggregate{Type: fleet.AggregateSatellite, ID: 7})
	require.NoError(t, err)
	require.Equal(t, rec.RenderedDigest, rec.PublishedDigest)
	require.Equal(t, fleet.Digest("sha256:pushed"), rec.PublishedArtifactDigest, "the manifest digest satellites report as applied")

	calls := len(p.registry.Calls())
	require.NoError(t, r.Reconcile(context.Background(), 7))
	require.Len(t, p.registry.Calls(), calls, "an unchanged satellite costs no registry call")
}

func TestSatelliteReconciler_GrouplessPublishesEmptyState(t *testing.T) {
	pending.Plan(t, "2.3", "D10: a satellite without groups gets no state artifact")
	p := newPlane()
	p.repo.Satellites.GroupsOf[7] = nil
	p.repo.Satellites.GrantsOf[7] = fleet.RobotGrant{Satellite: 7, HarborID: 42, RobotName: "robot$edge-01"}
	r := p.satellites(t)

	require.NoError(t, r.Reconcile(context.Background(), 7))

	require.Equal(t, 1, pushesTo(p.rec, ref("satellite", "edge-01")))
	payload, ok := p.rec.CallsTo("PushState")[0].Args[1].([]byte)
	require.True(t, ok)
	var doc map[string]any
	require.NoError(t, json.Unmarshal(payload, &doc))
	require.Empty(t, doc["states"])
	require.Equal(t, []string{fleet.SatelliteProject}, p.rec.CallsTo("SetRobotAccess")[0].Args[1])

	calls := len(p.registry.Calls())
	require.NoError(t, r.Reconcile(context.Background(), 7))
	require.Len(t, p.registry.Calls(), calls, "the empty state renders to the same digest, so it is not pushed again")
}

func TestSatelliteReconciler_FailureIsRetriedAndRecorded(t *testing.T) {
	pending.Plan(t, "2.3", pendingReconcilers)
	p := newPlane()
	r := p.satellites(t)
	boom := errors.New("registry down")
	p.registry.FailOn("PushState", boom)

	require.ErrorIs(t, r.Reconcile(context.Background(), 7), boom)
	rec, err := p.desired.Get(context.Background(), fleet.Aggregate{Type: fleet.AggregateSatellite, ID: 7})
	require.NoError(t, err)
	require.Contains(t, rec.LastError, "registry down")
	require.Empty(t, rec.PublishedDigest)

	p.registry.ClearFaults()
	require.NoError(t, r.Reconcile(context.Background(), 7))
	require.Equal(t, 2, pushesTo(p.rec, ref("satellite", "edge-01")), "the unpublished digest is pushed again")
}

func TestSatelliteReconciler_GoneSatelliteIsDone(t *testing.T) {
	pending.Plan(t, "2.3", pendingReconcilers)
	p := newPlane()

	require.NoError(t, p.satellites(t).Reconcile(context.Background(), 99))

	require.Empty(t, p.registry.Calls())
}

// A83: the reconciler creates a new satellite's robot, named per satellite
// ID. A crash between creating it and storing it leaves a robot the next
// attempt adopts, by exactly that name.
func TestSatelliteReconciler_CreatesTheRobotAndAdoptsItAfterACrash(t *testing.T) {
	pending.Plan(t, "2.5", "RegisterSatellite creates the robot in the request")
	p := newPlane()
	delete(p.repo.Robots.BySatelliteID, 7)
	delete(p.repo.Satellites.GrantsOf, 7)
	p.registry.Credentials = fleet.RobotCredentials{HarborID: 77, Name: "robot$edge-01-7", Secret: "s3cret"}
	r := p.satellites(t)
	crash := errors.New("process died")
	p.rec.FailOn("Robots.Add", crash)

	require.ErrorIs(t, r.Reconcile(context.Background(), 7), crash)
	spec, ok := onlyCallArg(t, p.rec, "CreateRobot").(fleet.RobotSpec)
	require.True(t, ok)
	require.Equal(t, fleet.RobotName(7, "edge-01"), spec.Name)
	require.Equal(t, settings.RobotDuration, spec.Duration)

	p.rec.ClearFaults()
	p.registry.FailOn("CreateRobot", fleet.ErrAlreadyExists)
	p.registry.Robots = map[string]fleet.RobotCredentials{"edge-01-7": {HarborID: 77, Name: "robot$edge-01-7"}}
	require.NoError(t, r.Reconcile(context.Background(), 7))

	finds := p.rec.CallsTo("FindRobot")
	require.Len(t, finds, 1)
	require.Equal(t, "edge-01-7", finds[0].Args[0], "never the bare satellite name of a deleted predecessor")
	adds := p.rec.CallsTo("Robots.Add")
	robot, ok := adds[len(adds)-1].Args[0].(fleet.Robot)
	require.True(t, ok)
	require.Equal(t, int64(77), robot.HarborID)
	require.Equal(t, fleet.SatelliteID(7), robot.Satellite)
	require.Equal(t, []string{"library", fleet.SatelliteProject, "team"}, grantOf(t, p.rec, 77), "however the calls are split (A73)")
}

// grantOf is what robot harborID may pull after the recorded calls: what
// CreateRobot gave it (the fake registry hands out one robot), replaced by
// every later SetRobotAccess for it.
func grantOf(t *testing.T, rec *fleettest.Recorder, harborID int64) []string {
	t.Helper()
	var projects []string
	granted := false
	for _, c := range rec.Calls() {
		switch c.Method {
		case "CreateRobot":
			spec, ok := c.Args[0].(fleet.RobotSpec)
			require.True(t, ok)
			projects, granted = spec.Projects, true
		case "SetRobotAccess":
			if c.Args[0] == harborID {
				p, ok := c.Args[1].([]string)
				require.True(t, ok)
				projects, granted = p, true
			}
		}
	}
	require.True(t, granted, "robot %d was never granted anything: %v", harborID, rec.Methods())
	return projects
}

func onlyCallArg(t *testing.T, rec *fleettest.Recorder, method string) any {
	t.Helper()
	calls := rec.CallsTo(method)
	require.NotEmpty(t, calls, "%s not called: %v", method, rec.Methods())
	return calls[0].Args[0]
}

// A85: the group reconciler publishes the group and fans out to its
// members in one statement; it reconciles none of them itself.
func TestGroupReconciler_PublishesAndFansOutToMembers(t *testing.T) {
	pending.Plan(t, "2.3", pendingReconcilers)
	p := newPlane()
	grp, err := async.NewGroupReconciler(p.repo, p.registry, p.desired, settings)
	require.NoError(t, err)

	require.NoError(t, grp.Reconcile(context.Background(), 1))

	require.Equal(t, 1, pushesTo(p.rec, ref("group", "g1")))
	fanOut := p.rec.CallsTo("Events.AppendToGroupMembers")
	require.Len(t, fanOut, 1)
	require.Equal(t, []any{fleet.GroupID(1), fleet.EventSatelliteChanged}, fanOut[0].Args)
	require.Zero(t, p.rec.Count("SetRobotAccess"), "members are reconciled from their own events")
	require.Equal(t, 1, p.rec.Count("Commit"), "the fan-out commits on its own")
}

func TestConfigReconciler_PublishesConfigState(t *testing.T) {
	pending.Plan(t, "2.3", pendingReconcilers)
	p := newPlane()
	cfg, err := async.NewConfigReconciler(p.repo, p.registry, p.desired, settings)
	require.NoError(t, err)

	require.NoError(t, cfg.Reconcile(context.Background(), 3))
	require.NoError(t, cfg.Reconcile(context.Background(), 3))

	require.Equal(t, 1, pushesTo(p.rec, ref("config", "edge-config")))
	payload, ok := p.rec.CallsTo("PushState")[0].Args[1].([]byte)
	require.True(t, ok)
	require.JSONEq(t, `{"app_config":{"log_level":"info"}}`, string(payload))
}

func TestTombstoneHandler_DeletesWhatThePayloadNames(t *testing.T) {
	pending.Plan(t, "2.3", pendingReconcilers)
	p := newPlane()
	p.repo.Satellites.ByName = map[string]fleet.Satellite{"edge-02": {ID: 8, Name: "edge-02"}}
	p.repo.Groups.ByName = nil
	p.repo.Configs.ByName = nil
	tomb := p.tombstones(t)
	ctx := context.Background()
	satellite := fleet.Aggregate{Type: fleet.AggregateSatellite, ID: 7}
	_, _, err := p.desired.Record(ctx, satellite, json.RawMessage(`{}`), "sha256:x")
	require.NoError(t, err)
	spiffeID := "spiffe://harbor-satellite.local/satellite/region/default/edge-01"

	require.NoError(t, tomb.Handle(ctx, event(t, 1, fleet.EventSatelliteDeleted, fleet.AggregateSatellite, 7,
		fleet.SatelliteDeletedPayload{Name: "edge-01", RobotHarborIDs: []int64{42, 44}, SPIFFEID: spiffeID, StateRef: ref("satellite", "edge-01")})))
	require.NoError(t, tomb.Handle(ctx, event(t, 2, fleet.EventGroupDeleted, fleet.AggregateGroup, 1,
		fleet.GroupDeletedPayload{Name: "g1", Members: []fleet.SatelliteID{8}, StateRef: ref("group", "g1")})))
	require.NoError(t, tomb.Handle(ctx, event(t, 3, fleet.EventConfigDeleted, fleet.AggregateConfig, 3,
		fleet.ConfigDeletedPayload{Name: "edge-config", StateRef: ref("config", "edge-config")})))

	var robots, states []any
	for _, c := range p.rec.CallsTo("DeleteRobot") {
		robots = append(robots, c.Args[0])
	}
	for _, c := range p.rec.CallsTo("DeleteState") {
		states = append(states, c.Args[0])
	}
	require.ElementsMatch(t, []any{int64(42), int64(44)}, robots, "every robot of the satellite")
	require.ElementsMatch(t, []any{ref("satellite", "edge-01"), ref("group", "g1"), ref("config", "edge-config")}, states)
	require.Equal(t, []any{spiffeID}, p.rec.CallsTo("DeleteEntriesFor")[0].Args)
	_, err = p.desired.Get(ctx, satellite)
	require.ErrorIs(t, err, fleet.ErrNotFound, "the desired-state row goes with the satellite")
	require.Equal(t, []fleet.Event{{
		Kind:      fleet.EventSatelliteChanged,
		Aggregate: fleet.Aggregate{Type: fleet.AggregateSatellite, ID: 8},
		Payload:   fleet.ChangedPayload{Name: "edge-02"},
	}}, p.repo.Events.Appended(), "the deleted group's members lose its projects")

	p.registry.FailOn("DeleteRobot", fleet.ErrNotFound)
	p.registry.FailOn("DeleteState", fleet.ErrNotFound)
	require.NoError(t, tomb.Handle(ctx, event(t, 4, fleet.EventSatelliteDeleted, fleet.AggregateSatellite, 7,
		fleet.SatelliteDeletedPayload{Name: "edge-01", RobotHarborIDs: []int64{42}, StateRef: ref("satellite", "edge-01")})),
		"objects already gone count as deleted")
}

// A84: edge-01 (7) is deleted and registered again as 9, which publishes
// first. The old tombstone must not delete what 9 published.
func TestTombstoneHandler_KeepsTheStateOfARecreatedName(t *testing.T) {
	pending.Plan(t, "2.3", pendingReconcilers)
	p := newPlane()
	p.repo.Satellites.ByName = map[string]fleet.Satellite{"edge-01": {ID: 9, Name: "edge-01"}}
	ctx := context.Background()
	reborn := fleet.Aggregate{Type: fleet.AggregateSatellite, ID: 9}
	_, _, err := p.desired.Record(ctx, reborn, json.RawMessage(`{}`), "sha256:nine")
	require.NoError(t, err)
	require.NoError(t, p.desired.MarkPublished(ctx, reborn, "sha256:nine", "sha256:manifest"))
	spiffeID := "spiffe://harbor-satellite.local/satellite/region/default/edge-01"

	require.NoError(t, p.tombstones(t).Handle(ctx, event(t, 1, fleet.EventSatelliteDeleted, fleet.AggregateSatellite, 7,
		fleet.SatelliteDeletedPayload{Name: "edge-01", RobotHarborIDs: []int64{42}, SPIFFEID: spiffeID, StateRef: ref("satellite", "edge-01")})))

	require.Zero(t, p.rec.Count("DeleteState"), "the artifact now belongs to satellite 9")
	require.Zero(t, p.rec.Count("DeleteEntriesFor"), "so does the SPIFFE ID")
	require.Equal(t, []any{int64(42)}, p.rec.CallsTo("DeleteRobot")[0].Args, "robots are per satellite ID")
	rec, err := p.desired.Get(ctx, reborn)
	require.NoError(t, err)
	require.Empty(t, rec.PublishedDigest, "satellite 9 publishes again")
}

// Level-triggered handlers make the order and number of events irrelevant:
// whatever arrives, each state is pushed once.
func TestReconcilers_DuplicateAndReorderedEventsHarmless(t *testing.T) {
	pending.Plan(t, "2.3", pendingReconcilers)
	events := []async.OutboxEvent{
		{ID: 1, Kind: fleet.EventGroupChanged, Aggregate: fleet.Aggregate{Type: fleet.AggregateGroup, ID: 1}},
		{ID: 2, Kind: fleet.EventSatelliteCreated, Aggregate: fleet.Aggregate{Type: fleet.AggregateSatellite, ID: 7}},
		{ID: 3, Kind: fleet.EventAssignmentChanged, Aggregate: fleet.Aggregate{Type: fleet.AggregateSatellite, ID: 7}},
		{ID: 4, Kind: fleet.EventConfigChanged, Aggregate: fleet.Aggregate{Type: fleet.AggregateConfig, ID: 3}},
		{ID: 5, Kind: fleet.EventGroupChanged, Aggregate: fleet.Aggregate{Type: fleet.AggregateGroup, ID: 1}},
		{ID: 6, Kind: fleet.EventSatelliteChanged, Aggregate: fleet.Aggregate{Type: fleet.AggregateSatellite, ID: 7}},
	}
	for seed := range uint64(5) {
		p := newPlane()
		router := p.routes(t)
		order := append([]async.OutboxEvent(nil), events...)
		rand.New(rand.NewPCG(seed, seed)).Shuffle(len(order), func(i, j int) { order[i], order[j] = order[j], order[i] })

		for _, e := range order {
			require.NoError(t, router.Handle(context.Background(), e), "seed %d event %d", seed, e.ID)
		}

		for _, target := range []fleet.ArtifactRef{ref("satellite", "edge-01"), ref("group", "g1"), ref("config", "edge-config")} {
			require.Equal(t, 1, pushesTo(p.rec, target), "seed %d: %s", seed, target)
		}
	}
}

func TestNewRoutes_HandlesEveryEventKind(t *testing.T) {
	pending.Plan(t, "2.3", pendingReconcilers)
	p := newPlane()
	router := p.routes(t)

	for _, kind := range fleet.EventKinds() {
		if kind.LogOnly() {
			continue
		}
		err := router.Handle(context.Background(), async.OutboxEvent{ID: 1, Kind: kind, Aggregate: fleet.Aggregate{Type: fleet.AggregateSatellite, ID: 7}, Payload: json.RawMessage(`{}`)})
		require.NotErrorIs(t, err, async.ErrNoHandler, "%s", kind)
	}
	require.ErrorIs(t, router.Handle(context.Background(), async.OutboxEvent{Kind: "Unknown"}), async.ErrNoHandler)
}

func TestRegistryProbe_RecordsLastSuccessfulPing(t *testing.T) {
	pending.Plan(t, "2.6", "/health checks only the database; nothing pings Harbor after startup")
	registry := fleettest.NewRegistry(nil)
	registry.FailOn("Ping", fleet.ErrRegistryUnavailable)
	probe, err := async.NewRegistryProbe(registry, 10*time.Millisecond, time.Second)
	require.NoError(t, err)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- probe.Run(ctx) }()
	defer func() {
		cancel()
		require.NoError(t, <-done)
	}()

	require.Eventually(t, func() bool { return registry.Count("Ping") >= 2 }, 5*time.Second, 5*time.Millisecond)
	require.True(t, probe.LastContact().IsZero(), "failed pings are no contact")
	require.False(t, probe.Reachable())

	registry.ClearFaults()
	start := time.Now()
	require.Eventually(t, func() bool { return !probe.LastContact().Before(start) }, 5*time.Second, 5*time.Millisecond)
	require.True(t, probe.Reachable())
}

func TestLimitRegistry_SpacesCalls(t *testing.T) {
	pending.Plan(t, "2.3", "nothing limits the request rate toward Harbor")
	registry := fleettest.NewRegistry(nil)
	limited := async.LimitRegistry(registry, 20)

	start := time.Now()
	for range 11 {
		require.NoError(t, limited.Ping(context.Background()))
	}

	require.GreaterOrEqual(t, time.Since(start), 450*time.Millisecond, "11 calls at 20/s take at least 0.5s")
	require.Equal(t, 11, registry.Count("Ping"))
}

const pendingRevoke = "ADR-0006 lifecycle is not implemented; a revoked satellite keeps an enabled robot"

func TestSatelliteReconciler_KindsIncludeLifecycle(t *testing.T) {
	var r *async.SatelliteReconciler
	kinds := r.Kinds()
	require.Contains(t, kinds, fleet.EventSatelliteRevoked)
	require.Contains(t, kinds, fleet.EventSatelliteUnrevoked)
	require.NotContains(t, kinds, fleet.EventSatelliteStale)
	require.True(t, fleet.EventSatelliteStale.LogOnly())
	require.False(t, fleet.EventSatelliteRevoked.LogOnly())
	require.False(t, fleet.EventSatelliteUnrevoked.LogOnly())
}

// lastAccess is the last SetRobotAccess call: its projects and disabled flag.
func lastAccess(t *testing.T, rec *fleettest.Recorder) ([]string, bool) {
	t.Helper()
	sets := rec.CallsTo("SetRobotAccess")
	require.NotEmpty(t, sets, "SetRobotAccess not called: %v", rec.Methods())
	last := sets[len(sets)-1]
	projects, ok := last.Args[1].([]string)
	require.True(t, ok)
	disabled, ok := last.Args[2].(bool)
	require.True(t, ok)
	return projects, disabled
}

func TestSatelliteReconciler_RevokedEndsDisabledWithProjects(t *testing.T) {
	pending.Plan(t, "4.3", pendingRevoke)
	p := newPlane()
	p.repo.Views.ByID = map[fleet.SatelliteID]fleet.SatelliteView{7: {Satellite: fleet.Satellite{ID: 7, Name: "edge-01"}, State: fleet.StateRevoked}}

	require.NoError(t, p.satellites(t).Handle(context.Background(),
		event(t, 1, fleet.EventSatelliteRevoked, fleet.AggregateSatellite, 7, fleet.SatelliteLifecyclePayload{Name: "edge-01", State: fleet.StateRevoked, PreviousState: fleet.StateOnline})))

	projects, disabled := lastAccess(t, p.rec)
	require.True(t, disabled)
	require.Equal(t, []string{"library", fleet.SatelliteProject, "team"}, projects, "Harbor rejects an empty permission list")
	payload, ok := p.rec.CallsTo("PushState")[0].Args[1].([]byte)
	require.True(t, ok)
	require.Contains(t, string(payload), `"robot_disabled":true`)
}

func TestSatelliteReconciler_UnrevokedEndsEnabled(t *testing.T) {
	pending.Plan(t, "4.3", pendingRevoke)
	p := newPlane()
	p.repo.Views.ByID = map[fleet.SatelliteID]fleet.SatelliteView{7: {Satellite: fleet.Satellite{ID: 7, Name: "edge-01"}, State: fleet.StatePending}}

	require.NoError(t, p.satellites(t).Handle(context.Background(),
		event(t, 1, fleet.EventSatelliteUnrevoked, fleet.AggregateSatellite, 7, fleet.SatelliteLifecyclePayload{Name: "edge-01", State: fleet.StatePending, PreviousState: fleet.StateRevoked})))

	projects, disabled := lastAccess(t, p.rec)
	require.False(t, disabled)
	require.Equal(t, []string{"library", fleet.SatelliteProject, "team"}, projects)
}

func TestSatelliteReconciler_RobotRotatedClearsAndRegrants(t *testing.T) {
	pending.Plan(t, "8.4", pendingRotation)
	p := newPlane()
	r := p.satellites(t)
	require.NoError(t, r.Reconcile(context.Background(), 7))
	pushes := pushesTo(p.rec, ref("satellite", "edge-01"))
	p.repo.Robots.BySatelliteID[7] = fleet.Robot{ID: 5, Satellite: 7, HarborID: 43}
	grant := p.repo.Satellites.GrantsOf[7]
	grant.HarborID = 43
	p.repo.Satellites.GrantsOf[7] = grant

	require.NoError(t, r.Handle(context.Background(),
		event(t, 2, fleet.EventRobotRotated, fleet.AggregateSatellite, 7, fleet.RobotRotatedPayload{Name: "edge-01", Generation: 1, RobotHarborIDs: []int64{42, 43}})))

	sets := p.rec.CallsTo("SetRobotAccess")
	last := sets[len(sets)-1]
	require.Equal(t, int64(43), last.Args[0], "the successor is granted")
	require.Equal(t, []string{"library", fleet.SatelliteProject, "team"}, last.Args[1])
	require.Equal(t, false, last.Args[2])
	require.Equal(t, pushes+1, pushesTo(p.rec, ref("satellite", "edge-01")), "the cleared digest republishes the unchanged document once")
}
