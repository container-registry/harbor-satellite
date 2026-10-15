package fleet_test

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/container-registry/harbor-satellite/internal/groundcontrol/fleet"
	"github.com/container-registry/harbor-satellite/internal/groundcontrol/fleet/fleettest"
	"github.com/stretchr/testify/require"
)

const registryURL = "http://harbor.example"

var now = time.Date(2026, 10, 15, 12, 0, 0, 0, time.UTC)

// world is a set of fakes that record into one sequence.
type world struct {
	rec      *fleettest.Recorder
	repo     *fleettest.Repository
	registry *fleettest.Registry
	identity *fleettest.WorkloadIdentity
}

func newWorld() *world {
	rec := fleettest.NewRecorder()
	return &world{
		rec:      rec,
		repo:     fleettest.NewRepository(rec),
		registry: fleettest.NewRegistry(rec),
		identity: fleettest.NewWorkloadIdentity(rec),
	}
}

func (w *world) services(t *testing.T) *fleet.Services {
	t.Helper()
	svc, err := fleet.NewServices(fleet.Deps{
		Repository: w.repo,
		Registry:   w.registry,
		Identity:   w.identity,
		Clock:      fleettest.Clock{T: now},
		Settings: fleet.Settings{
			RegistryURL:   registryURL,
			RobotDuration: 30 * 24 * time.Hour,
			TokenTTL:      fleet.DefaultTokenTTL,
		},
	})
	require.NoError(t, err)
	return svc
}

func stateRef(kind, name string) fleet.ArtifactRef {
	return fleet.ArtifactRef{Registry: registryURL, Repository: "satellite/" + kind + "-state/" + name + "/state", Tag: "latest"}
}

// arg returns argument i of call as a T.
func arg[T any](t *testing.T, call fleettest.Call, i int) T {
	t.Helper()
	require.Greater(t, len(call.Args), i, "%s has no argument %d", call.Method, i)
	v, ok := call.Args[i].(T)
	require.True(t, ok, "%s argument %d is %T", call.Method, i, call.Args[i])
	return v
}

// onlyCall asserts that method was called once and returns that call.
func onlyCall(t *testing.T, rec *fleettest.Recorder, method string) fleettest.Call {
	t.Helper()
	calls := rec.CallsTo(method)
	require.Len(t, calls, 1, "calls to %s in %v", method, rec.Methods())
	return calls[0]
}

// pushedSatelliteState decodes the satellite state document of a PushState
// call: the group state URLs and the config state URL.
func pushedSatelliteState(t *testing.T, push fleettest.Call) (states []string, config string) {
	t.Helper()
	var doc struct {
		Config string   `json:"config"`
		States []string `json:"states"`
	}
	require.NoError(t, json.Unmarshal(arg[[]byte](t, push, 1), &doc))
	return doc.States, doc.Config
}

// outsideTx asserts that method is never called between a WithTx and its
// Commit or Rollback.
func outsideTx(t *testing.T, rec *fleettest.Recorder, method string) {
	t.Helper()
	inTx := false
	for _, m := range rec.Methods() {
		switch m {
		case "WithTx":
			inTx = true
		case "Commit", "Rollback":
			inTx = false
		case method:
			require.False(t, inTx, "%s runs inside a transaction: %v", method, rec.Methods())
		}
	}
}

// grantOf is what the robot with harborID may pull after the recorded
// calls: what CreateRobot gave it (the fake registry hands out one robot),
// replaced by every later SetRobotProjects for it. How a service splits
// the calls does not matter (A73).
func grantOf(t *testing.T, rec *fleettest.Recorder, harborID int64) []string {
	t.Helper()
	var projects []string
	granted := false
	for _, c := range rec.Calls() {
		switch c.Method {
		case "CreateRobot":
			projects, granted = arg[fleet.RobotSpec](t, c, 0).Projects, true
		case "SetRobotProjects":
			if arg[int64](t, c, 0) == harborID {
				projects, granted = arg[[]string](t, c, 1), true
			}
		}
	}
	require.True(t, granted, "robot %d was never granted anything: %v", harborID, rec.Methods())
	return projects
}

// before asserts that every call to first precedes every call to then.
func before(t *testing.T, rec *fleettest.Recorder, first, then string) {
	t.Helper()
	methods := rec.Methods()
	lastFirst, firstThen := -1, -1
	for i, m := range methods {
		if m == first {
			lastFirst = i
		}
		if m == then && firstThen < 0 {
			firstThen = i
		}
	}
	require.GreaterOrEqual(t, lastFirst, 0, "%s not called: %v", first, methods)
	require.GreaterOrEqual(t, firstThen, 0, "%s not called: %v", then, methods)
	require.Less(t, lastFirst, firstThen, "%s must come before %s: %v", first, then, methods)
}
