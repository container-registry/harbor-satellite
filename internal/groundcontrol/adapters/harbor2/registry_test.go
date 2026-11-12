package harbor2_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/container-registry/harbor-satellite/internal/groundcontrol/adapters/harbor2"
	"github.com/container-registry/harbor-satellite/internal/groundcontrol/fleet"
	"github.com/container-registry/harbor-satellite/internal/groundcontrol/harbortest"
	"github.com/container-registry/harbor-satellite/internal/shared/testutil/pending"
	"github.com/stretchr/testify/require"
)

const pendingAdapter = "the harbor2 adapter is a stub; GC uses the global harbor client"

func newRegistry(t *testing.T, stub *harbortest.Stub) *harbor2.Registry {
	t.Helper()
	r, err := harbor2.New(harbor2.Config{
		URL:      stub.URL(),
		Username: harbortest.Username,
		Password: harbortest.Password,
		Insecure: true,
	})
	require.NoError(t, err)
	return r
}

func stateRef(stub *harbortest.Stub, kind, name string) fleet.ArtifactRef {
	return fleet.ArtifactRef{Registry: stub.URL(), Repository: "satellite/" + kind + "-state/" + name + "/state", Tag: "latest"}
}

func TestRegistry_CreateRobotLikeTheRobotTemplate(t *testing.T) {
	pending.Plan(t, "1.3", pendingAdapter)
	stub := harbortest.New(t)
	r := newRegistry(t, stub)

	creds, err := r.CreateRobot(context.Background(), fleet.RobotSpec{Name: "edge-01", Projects: []string{fleet.SatelliteProject}, Duration: 30 * 24 * time.Hour})

	require.NoError(t, err)
	require.Equal(t, harbortest.RobotPrefix+"edge-01", creds.Name)
	require.NotEmpty(t, creds.Secret)
	require.NotZero(t, creds.HarborID)
	robot, ok := stub.Robot(creds.Name)
	require.True(t, ok)
	require.Equal(t, "system", robot.Level)
	require.NotNil(t, robot.Duration)
	require.Equal(t, int64(30), *robot.Duration)
	require.Equal(t, []string{fleet.SatelliteProject}, stub.RobotProjects(creds.Name))
	require.Equal(t, 1, stub.Count(harbortest.RouteCreateRobot))
	found, err := r.FindRobot(context.Background(), "edge-01")
	require.NoError(t, err)
	require.Equal(t, creds.HarborID, found.HarborID)
	require.Empty(t, found.Secret)
	_, err = r.CreateRobot(context.Background(), fleet.RobotSpec{Name: "edge-01", Projects: []string{fleet.SatelliteProject}, Duration: 30 * 24 * time.Hour})
	require.ErrorIs(t, err, fleet.ErrAlreadyExists)
	_, err = r.FindRobot(context.Background(), "edge-02")
	require.ErrorIs(t, err, fleet.ErrNotFound)
}

func TestRegistry_SetRobotAccessReadsThenReplaces(t *testing.T) {
	pending.Plan(t, "1.3", pendingAdapter)
	stub := harbortest.New(t)
	seeded := stub.SeedRobot(harbortest.RobotPrefix+"edge-01", fleet.SatelliteProject)
	r := newRegistry(t, stub)
	mark := stub.Mark()

	require.NoError(t, r.SetRobotAccess(context.Background(), seeded.ID, []string{"library", "team", fleet.SatelliteProject}, false))

	var routes []string
	for _, c := range stub.CallsSince(mark) {
		routes = append(routes, c.Route)
	}
	require.Equal(t, []string{harbortest.RouteGetRobot, harbortest.RouteUpdateRobot}, routes)
	require.Equal(t, []string{"library", fleet.SatelliteProject, "team"}, stub.RobotProjects(seeded.Name))
}

// Harbor rejects an empty permission list, so revoking disables the robot
// in the same PUT and keeps its projects.
func TestRegistry_SetRobotAccessOnePutKeepsOtherFields(t *testing.T) {
	pending.Plan(t, "4.3", pendingAdapter)
	stub := harbortest.New(t)
	seeded := stub.SeedRobot(harbortest.RobotPrefix+"edge-01", "library", fleet.SatelliteProject)
	r := newRegistry(t, stub)
	mark := stub.Mark()

	require.NoError(t, r.SetRobotAccess(context.Background(), seeded.ID, []string{"library", fleet.SatelliteProject}, true))

	var updates int
	for _, c := range stub.CallsSince(mark) {
		if c.Route == harbortest.RouteUpdateRobot {
			updates++
		}
	}
	require.Equal(t, 1, updates)
	robot, ok := stub.Robot(seeded.Name)
	require.True(t, ok)
	require.True(t, robot.Disable)
	require.Equal(t, []string{"library", fleet.SatelliteProject}, stub.RobotProjects(seeded.Name))
	require.Equal(t, seeded.Duration, robot.Duration)
	require.Equal(t, seeded.Level, robot.Level)

	require.NoError(t, r.SetRobotAccess(context.Background(), seeded.ID, []string{"library", fleet.SatelliteProject}, false))
	robot, _ = stub.Robot(seeded.Name)
	require.False(t, robot.Disable)
}

func TestRegistry_RotateRobotSecretReturnsTheNewSecret(t *testing.T) {
	pending.Plan(t, "1.3", pendingAdapter)
	stub := harbortest.New(t)
	seeded := stub.SeedRobot(harbortest.RobotPrefix+"edge-01", fleet.SatelliteProject)
	r := newRegistry(t, stub)

	creds, err := r.RotateRobotSecret(context.Background(), seeded.ID)

	require.NoError(t, err)
	require.Equal(t, seeded.ID, creds.HarborID)
	require.NotEmpty(t, creds.Secret)
	require.NotEqual(t, seeded.Secret, creds.Secret)
	require.Equal(t, 1, stub.Count(harbortest.RouteRefreshRobot))
}

func TestRegistry_PushStateTagsLatestAndPushTime(t *testing.T) {
	pending.Plan(t, "1.3", pendingAdapter)
	stub := harbortest.New(t)
	stub.AddProject(fleet.SatelliteProject)
	r := newRegistry(t, stub)
	ref := stateRef(stub, "group", "g1")
	payload := []byte(`{"group":"g1"}`)

	digest, err := r.PushState(context.Background(), ref, payload)

	require.NoError(t, err)
	require.True(t, strings.HasPrefix(string(digest), "sha256:"), digest)
	require.Equal(t, 3, stub.Count(harbortest.RoutePutManifest), "push plus two tags, as pushStateArtifact")
	got, err := stub.ArtifactFile(ref.Repository, "latest", "artifacts.json")
	require.NoError(t, err)
	require.Equal(t, payload, got)
}

func TestRegistry_DeleteStateRemovesTheRepository(t *testing.T) {
	pending.Plan(t, "1.3", pendingAdapter)
	stub := harbortest.New(t)
	stub.AddProject(fleet.SatelliteProject)
	r := newRegistry(t, stub)
	ref := stateRef(stub, "config", "edge-config")
	_, err := r.PushState(context.Background(), ref, []byte(`{}`))
	require.NoError(t, err)

	require.NoError(t, r.DeleteState(context.Background(), ref))

	require.False(t, stub.HasRepository(ref.Repository))
	require.Equal(t, 1, stub.Count(harbortest.RouteDeleteRepository))
}

func TestRegistry_EnsureProjectCreatesOnlyWhenMissing(t *testing.T) {
	pending.Plan(t, "1.3", pendingAdapter)
	stub := harbortest.New(t)
	r := newRegistry(t, stub)

	require.NoError(t, r.EnsureProject(context.Background(), fleet.SatelliteProject))
	require.NoError(t, r.EnsureProject(context.Background(), fleet.SatelliteProject))

	require.True(t, stub.HasProject(fleet.SatelliteProject))
	require.Equal(t, 1, stub.Count(harbortest.RouteCreateProject))
}

func TestRegistry_UnreachableIsErrRegistryUnavailable(t *testing.T) {
	pending.Plan(t, "1.3", pendingAdapter)
	stub := harbortest.New(t)
	r := newRegistry(t, stub)
	stub.SetDown(true)

	require.ErrorIs(t, r.Ping(context.Background()), fleet.ErrRegistryUnavailable)
	_, err := r.FindRobot(context.Background(), "edge-01")
	require.ErrorIs(t, err, fleet.ErrRegistryUnavailable)
}

// Two registries in one process must not share a client: today's
// harbor.GetClient is a sync.Once bound to the first URL it saw.
func TestRegistry_InstancesUseTheirOwnHarbor(t *testing.T) {
	pending.Plan(t, "1.3", pendingAdapter)
	first, second := harbortest.New(t), harbortest.New(t)
	a, b := newRegistry(t, first), newRegistry(t, second)

	require.NoError(t, a.EnsureProject(context.Background(), "alpha"))
	require.NoError(t, b.EnsureProject(context.Background(), "beta"))

	require.True(t, first.HasProject("alpha"))
	require.False(t, first.HasProject("beta"))
	require.True(t, second.HasProject("beta"))
	require.False(t, second.HasProject("alpha"))
}

// Tombstones run again after a crash; an object that is already gone must
// look like it to them (fleet.ErrNotFound), not like a failure.
func TestRegistry_MissingObjectsAreErrNotFound(t *testing.T) {
	pending.Plan(t, "2.3", pendingAdapter)
	stub := harbortest.New(t)
	stub.AddProject(fleet.SatelliteProject)
	r := newRegistry(t, stub)

	require.ErrorIs(t, r.DeleteRobot(context.Background(), 4711), fleet.ErrNotFound)
	require.ErrorIs(t, r.DeleteState(context.Background(), stateRef(stub, "satellite", "never-pushed")), fleet.ErrNotFound)
	_, err := r.RotateRobotSecret(context.Background(), 4711)
	require.ErrorIs(t, err, fleet.ErrNotFound)
	require.ErrorIs(t, r.SetRobotAccess(context.Background(), 4711, []string{fleet.SatelliteProject}, false), fleet.ErrNotFound)
}
