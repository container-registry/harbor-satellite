package postgres_test

import (
	"context"
	"fmt"
	"testing"

	"github.com/container-registry/harbor-satellite/internal/groundcontrol/fleet"
	"github.com/container-registry/harbor-satellite/internal/shared/testutil/pending"
	"github.com/stretchr/testify/require"
)

const pendingPolicies = "the postgres policy repository is a stub; policies are not stored"

func TestPolicyRepo_RoundTrip(t *testing.T) {
	pending.Plan(t, "7.3", pendingPolicies)
	repo := newRepository(t)
	ctx := context.Background()
	policies := repo.Autocommit().Policies()
	rule := fleet.PolicyRule{Subject: "role:edge-admins", Scope: "group:g1", Type: "satellite", Action: "read", Effect: fleet.EffectAllow}

	added, err := policies.AddRule(ctx, rule)
	require.NoError(t, err)
	require.NotZero(t, added.ID)
	_, err = policies.AddRule(ctx, rule)
	require.ErrorIs(t, err, fleet.ErrAlreadyExists)
	binding, err := policies.AddBinding(ctx, fleet.RoleBinding{Principal: "user:alice", Role: "role:edge-admins"})
	require.NoError(t, err)

	rules, err := policies.ListRules(ctx)
	require.NoError(t, err)
	rule.ID = added.ID
	require.Equal(t, []fleet.PolicyRule{rule}, rules)
	bindings, err := policies.ListBindings(ctx)
	require.NoError(t, err)
	require.Equal(t, []fleet.RoleBinding{binding}, bindings)

	v0, err := policies.Version(ctx)
	require.NoError(t, err)
	v1, err := policies.BumpVersion(ctx)
	require.NoError(t, err)
	require.Equal(t, v0+1, v1)

	require.NoError(t, policies.DeleteRule(ctx, added.ID))
	require.ErrorIs(t, policies.DeleteRule(ctx, added.ID), fleet.ErrNotFound)
	require.NoError(t, policies.DeleteBinding(ctx, binding.ID))
	require.ErrorIs(t, policies.DeleteBinding(ctx, binding.ID), fleet.ErrNotFound)
}

// A snapshot taken while a write is in flight shows either the old version
// with the old rules or the new version with the new rules, never a mix.
func TestPolicyRepo_SnapshotIsConsistent(t *testing.T) {
	pending.Plan(t, "7.3", pendingPolicies)
	repo := newRepository(t)
	ctx := context.Background()
	rule := fleet.PolicyRule{Subject: "role:edge-admins", Scope: "*", Type: "group", Action: "read", Effect: fleet.EffectAllow}

	var during fleet.PolicySnapshot
	require.NoError(t, repo.WithTx(ctx, func(tx fleet.Tx) error {
		if _, err := tx.Policies().AddRule(ctx, rule); err != nil {
			return err
		}
		if _, err := tx.Policies().BumpVersion(ctx); err != nil {
			return err
		}
		var err error
		during, err = repo.Autocommit().Policies().Snapshot(ctx)
		return err
	}))
	after, err := repo.Autocommit().Policies().Snapshot(ctx)
	require.NoError(t, err)

	require.Empty(t, during.Rules, "the uncommitted write is invisible")
	require.Equal(t, after.Version-1, during.Version)
	require.Len(t, after.Rules, 1)
}

// Each write adds a rule and bumps the version in one transaction, so a
// snapshot, one REPEATABLE READ, always holds as many rules as versions
// passed; reading the two in separate statements would let a commit in
// between show a mix.
func TestPolicyRepo_SnapshotMatchesItsVersionUnderConcurrentWrites(t *testing.T) {
	pending.Plan(t, "7.3", pendingPolicies)
	repo := newRepository(t)
	ctx := context.Background()
	base, err := repo.Autocommit().Policies().Version(ctx)
	require.NoError(t, err)
	done := make(chan error, 1)
	go func() {
		for i := range 50 {
			rule := fleet.PolicyRule{Subject: fmt.Sprintf("role:r%d", i), Scope: "*", Type: "group", Action: "read", Effect: fleet.EffectAllow}
			if err := repo.WithTx(ctx, func(tx fleet.Tx) error {
				if _, err := tx.Policies().AddRule(ctx, rule); err != nil {
					return err
				}
				_, err := tx.Policies().BumpVersion(ctx)
				return err
			}); err != nil {
				done <- err
				return
			}
		}
		done <- nil
	}()

	for {
		s, err := repo.Autocommit().Policies().Snapshot(ctx)
		require.NoError(t, err)
		require.EqualValues(t, s.Version-base, len(s.Rules), "version %d", s.Version)
		select {
		case err := <-done:
			require.NoError(t, err)
			return
		default:
		}
	}
}
