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

func TestRegistry_SetRobotProjectsReadsThenReplaces(t *testing.T) {
	pending.Plan(t, "1.3", pendingAdapter)
	stub := harbortest.New(t)
	seeded := stub.SeedRobot(harbortest.RobotPrefix+"edge-01", fleet.SatelliteProject)
	r := newRegistry(t, stub)
	mark := stub.Mark()

	require.NoError(t, r.SetRobotProjects(context.Background(), seeded.ID, []string{"library", "team", fleet.SatelliteProject}))

	var routes []string
	for _, c := range stub.CallsSince(mark) {
		routes = append(routes, c.Route)
	}
	require.Equal(t, []string{harbortest.RouteGetRobot, harbortest.RouteUpdateRobot}, routes)
	require.Equal(t, []string{"library", fleet.SatelliteProject, "team"}, stub.RobotProjects(seeded.Name))
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
