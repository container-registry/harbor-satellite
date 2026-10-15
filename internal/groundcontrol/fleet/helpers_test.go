package fleet_test

import (
	"slices"
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
	sessions *fleettest.SessionIssuer
	beats    *fleettest.HeartbeatSink
}

func newWorld() *world {
	rec := fleettest.NewRecorder()
	return &world{
		rec:      rec,
		repo:     fleettest.NewRepository(rec),
		registry: fleettest.NewRegistry(rec),
		identity: fleettest.NewWorkloadIdentity(rec),
		sessions: fleettest.NewSessionIssuer(rec),
		beats:    fleettest.NewHeartbeatSink(rec),
	}
}

func (w *world) services(t *testing.T) *fleet.Services {
	t.Helper()
	svc, err := fleet.NewServices(fleet.Deps{
		Repository:    w.repo,
		Registry:      w.registry,
		Identity:      w.identity,
		Sessions:      w.sessions,
		HeartbeatSink: w.beats,
		Clock:         fleettest.Clock{T: now},
		Settings: fleet.Settings{
			RegistryURL:            registryURL,
			RobotDuration:          30 * 24 * time.Hour,
			TokenTTL:               fleet.DefaultTokenTTL,
			StaleThreshold:         15 * time.Minute,
			HeartbeatFlushInterval: 5 * time.Second,
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

// registryMethods are the fleet.Registry calls; the 2.5 services make none
// in a request except ZeroTouch's rotation.
var registryMethods = []string{
	"FindRobot", "CreateRobot", "SetRobotAccess", "RotateRobotSecret", "DeleteRobot",
	"PushState", "DeleteState", "EnsureProject", "ReplicationPolicyExists", "Ping",
}

// noRegistryCalls asserts that the request called the registry only with
// the allowed methods.
func noRegistryCalls(t *testing.T, rec *fleettest.Recorder, allowed ...string) {
	t.Helper()
	for _, m := range registryMethods {
		if slices.Contains(allowed, m) {
			continue
		}
		require.Zero(t, rec.Count(m), "%s in the request: %v", m, rec.Methods())
	}
}

// insideTx asserts that every call of method happens between a WithTx and
// its Commit.
func insideTx(t *testing.T, rec *fleettest.Recorder, method string) {
	t.Helper()
	inTx, seen := false, false
	for _, m := range rec.Methods() {
		switch m {
		case "WithTx":
			inTx = true
		case "Commit", "Rollback":
			inTx = false
		case method:
			seen = true
			require.True(t, inTx, "%s runs outside a transaction: %v", method, rec.Methods())
		}
	}
	require.True(t, seen, "%s not called: %v", method, rec.Methods())
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
