package async_test

import (
	"context"
	"testing"
	"time"

	"github.com/container-registry/harbor-satellite/internal/groundcontrol/async"
	"github.com/container-registry/harbor-satellite/internal/groundcontrol/fleet"
	"github.com/container-registry/harbor-satellite/internal/groundcontrol/fleet/fleettest"
	"github.com/container-registry/harbor-satellite/internal/shared/testutil/pending"
	"github.com/stretchr/testify/require"
)

const pendingRotation = "robots are not rotated; the satellite refreshes its secret in the request (D2)"

var rotationNow = time.Date(2026, 10, 15, 12, 0, 0, 0, time.UTC)

// rotationStore records into the plane's recorder as "Rotations.<Method>"
// and answers from its fields.
type rotationStore struct {
	rec      *fleettest.Recorder
	expiring []fleet.Robot
	pending  []async.PendingRotation
	acked    []fleet.RobotRotation
}

func (s *rotationStore) ExpiringBefore(_ context.Context, now, before time.Time, limit int) ([]fleet.Robot, error) {
	if err := s.rec.Record("Rotations.ExpiringBefore", now, before, limit); err != nil {
		return nil, err
	}
	return s.expiring, nil
}

func (s *rotationStore) Stage(_ context.Context, rotation fleet.RobotRotation, sealedSecret []byte, secretHash string) (int64, error) {
	if err := s.rec.Record("Rotations.Stage", rotation, sealedSecret, secretHash); err != nil {
		return 0, err
	}
	return rotation.Generation, nil
}

func (s *rotationStore) Pending(context.Context) ([]async.PendingRotation, error) {
	if err := s.rec.Record("Rotations.Pending"); err != nil {
		return nil, err
	}
	return s.pending, nil
}

func (s *rotationStore) Acked(_ context.Context, limit int) ([]fleet.RobotRotation, error) {
	if err := s.rec.Record("Rotations.Acked", limit); err != nil {
		return nil, err
	}
	return s.acked, nil
}

func (s *rotationStore) Promote(_ context.Context, rotation fleet.RobotRotation) error {
	return s.rec.Record("Rotations.Promote", rotation)
}

// sealer prefixes the plaintext, so tests can tell sealed from plain.
type sealer struct{}

func (sealer) Seal(plaintext []byte) ([]byte, error) {
	return append([]byte("sealed:"), plaintext...), nil
}

func (sealer) Open(ciphertext []byte) ([]byte, error) {
	return ciphertext[len("sealed:"):], nil
}

func newRotator(t *testing.T, p *plane, store *rotationStore) *async.RobotRotator {
	t.Helper()
	r, err := async.NewRobotRotator(store, p.registry, newFakeLocker(), sealer{}, fleettest.Clock{T: rotationNow}, 72*time.Hour, time.Minute)
	require.NoError(t, err)
	return r
}

// expiringEdge01 is edge-01's robot 42 expiring within the rotation window.
func expiringEdge01() fleet.Robot {
	return fleet.Robot{ID: 5, Satellite: 7, Name: "robot$edge-01-7", HarborID: 42, ExpiresAt: rotationNow.Add(24 * time.Hour)}
}

func TestRobotRotator_StagesSuccessorBeforeExpiry(t *testing.T) {
	pending.Plan(t, "8.4", pendingRotation)
	p := newPlane()
	p.registry.Credentials = fleet.RobotCredentials{HarborID: 43, Name: "robot$edge-01-7-g1", Secret: "successor", ExpiresAt: rotationNow.Add(30 * 24 * time.Hour)}
	store := &rotationStore{rec: p.rec, expiring: []fleet.Robot{expiringEdge01()}}

	staged, promoted, err := newRotator(t, p, store).RunOnce(context.Background())

	require.NoError(t, err)
	require.Equal(t, 1, staged)
	require.Zero(t, promoted)
	spec, ok := p.rec.CallsTo("CreateRobot")[0].Args[0].(fleet.RobotSpec)
	require.True(t, ok)
	require.Equal(t, fleet.SuccessorRobotName(7, "edge-01", 1), spec.Name)
	require.Equal(t, []string{"library", fleet.SatelliteProject, "team"}, spec.Projects, "the current RobotProjects")
	stage := p.rec.CallsTo("Rotations.Stage")
	require.Len(t, stage, 1)
	rotation, ok := stage[0].Args[0].(fleet.RobotRotation)
	require.True(t, ok)
	require.Equal(t, int64(43), rotation.Successor.HarborID)
	require.Equal(t, []byte("sealed:successor"), stage[0].Args[1], "only the sealed secret is stored")
	require.NotEqual(t, "successor", stage[0].Args[2])
	require.Zero(t, p.rec.Count("DeleteRobot"), "the old robot stays until the ack")
}

// A crash between CreateRobot and Stage left the successor in Harbor with
// its secret lost: the retry adopts it and replaces the secret.
func TestRobotRotator_CrashBeforeStageAdoptsSuccessor(t *testing.T) {
	pending.Plan(t, "8.4", pendingRotation)
	p := newPlane()
	name := fleet.SuccessorRobotName(7, "edge-01", 1)
	p.registry.FailOn("CreateRobot", fleet.ErrAlreadyExists)
	p.registry.Robots = map[string]fleet.RobotCredentials{name: {HarborID: 43, Name: name}}
	p.registry.Credentials = fleet.RobotCredentials{HarborID: 43, Name: name, Secret: "replaced"}
	store := &rotationStore{rec: p.rec, expiring: []fleet.Robot{expiringEdge01()}}

	staged, _, err := newRotator(t, p, store).RunOnce(context.Background())

	require.NoError(t, err)
	require.Equal(t, 1, staged)
	require.Equal(t, name, p.rec.CallsTo("FindRobot")[0].Args[0])
	require.Equal(t, int64(43), p.rec.CallsTo("RotateRobotSecret")[0].Args[0])
	require.Equal(t, []byte("sealed:replaced"), p.rec.CallsTo("Rotations.Stage")[0].Args[1])
}

func ackedEdge01() fleet.RobotRotation {
	return fleet.RobotRotation{
		Satellite: 7, Generation: 1,
		Successor: fleet.RobotCredentials{HarborID: 43, Name: fleet.SuccessorRobotName(7, "edge-01", 1)},
		Previous:  expiringEdge01(),
	}
}

func TestRobotRotator_PromoteDeletesOldThenPromotes(t *testing.T) {
	pending.Plan(t, "8.4", pendingRotation)
	p := newPlane()
	store := &rotationStore{rec: p.rec, acked: []fleet.RobotRotation{ackedEdge01()}}

	_, promoted, err := newRotator(t, p, store).RunOnce(context.Background())

	require.NoError(t, err)
	require.Equal(t, 1, promoted)
	require.Equal(t, int64(42), p.rec.CallsTo("DeleteRobot")[0].Args[0])
	require.Len(t, p.rec.CallsTo("Rotations.Promote"), 1)
	methods := p.rec.Methods()
	del, prom := -1, -1
	for i, m := range methods {
		switch m {
		case "DeleteRobot":
			del = i
		case "Rotations.Promote":
			prom = i
		}
	}
	require.Less(t, del, prom, "the old robot goes before the promotion: %v", methods)
}

func TestRobotRotator_DeletedOldRobotStillPromotes(t *testing.T) {
	pending.Plan(t, "8.4", pendingRotation)
	p := newPlane()
	p.registry.FailOn("DeleteRobot", fleet.ErrNotFound)
	store := &rotationStore{rec: p.rec, acked: []fleet.RobotRotation{ackedEdge01()}}

	_, promoted, err := newRotator(t, p, store).RunOnce(context.Background())

	require.NoError(t, err)
	require.Equal(t, 1, promoted)
	require.Len(t, p.rec.CallsTo("Rotations.Promote"), 1)
}

func TestRobotRotator_RegistryDownSkips(t *testing.T) {
	pending.Plan(t, "8.4", pendingRotation)
	p := newPlane()
	p.registry.FailOn("CreateRobot", fleet.ErrRegistryUnavailable)
	p.registry.FailOn("DeleteRobot", fleet.ErrRegistryUnavailable)
	store := &rotationStore{rec: p.rec, expiring: []fleet.Robot{expiringEdge01()}, acked: []fleet.RobotRotation{ackedEdge01()}}

	staged, promoted, err := newRotator(t, p, store).RunOnce(context.Background())

	require.NoError(t, err, "an outage is skipped, not failed")
	require.Zero(t, staged)
	require.Zero(t, promoted)
	require.Zero(t, p.rec.Count("Rotations.Stage"))
	require.Zero(t, p.rec.Count("Rotations.Promote"))
}

func TestRotationCache_ServesWithoutStatements(t *testing.T) {
	pending.Plan(t, "8.4", pendingRotation)
	p := newPlane()
	store := &rotationStore{rec: p.rec, pending: []async.PendingRotation{{
		Satellite: 7, Generation: 2, Username: "robot$edge-01-7-g2", SealedSecret: []byte("sealed:successor"), ExpiresAt: rotationNow.Add(time.Hour),
	}}}
	cache, err := async.NewRotationCache(store, sealer{}, 10*time.Millisecond)
	require.NoError(t, err)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- cache.Run(ctx) }()

	require.Eventually(t, func() bool { _, ok := cache.Pending(7); return ok }, time.Second, 5*time.Millisecond)
	cancel()
	require.NoError(t, <-done)
	refreshes := p.rec.Count("Rotations.Pending")
	got, ok := cache.Pending(7)
	_, other := cache.Pending(8)
	for range 10 {
		cache.Pending(7)
	}

	require.True(t, ok)
	require.False(t, other)
	require.Equal(t, fleet.RotatedCredentials{Username: "robot$edge-01-7-g2", Password: "successor", ExpiresAt: rotationNow.Add(time.Hour), Generation: 2}, got)
	require.Equal(t, refreshes, p.rec.Count("Rotations.Pending"), "Pending reads the cache, never the store")
}
