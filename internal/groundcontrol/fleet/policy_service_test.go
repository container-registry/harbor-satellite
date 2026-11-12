package fleet_test

import (
	"context"
	"testing"

	"github.com/container-registry/harbor-satellite/internal/groundcontrol/fleet"
	"github.com/container-registry/harbor-satellite/internal/shared/testutil/pending"
	"github.com/stretchr/testify/require"
)

const pendingPolicies = "policies are not stored; authorization is the hard-coded system_admin check"

func validRule() fleet.PolicyRule {
	return fleet.PolicyRule{Subject: "role:edge-admins", Scope: "group:g1", Type: "satellite", Action: "read", Effect: fleet.EffectAllow}
}

func TestPolicyService_WriteBumpsVersionAndAppendsLogOnlyEvent(t *testing.T) {
	pending.Plan(t, "7.3", pendingPolicies)
	w := newWorld()
	w.repo.Policies.CurrentVersion = 4
	w.repo.Policies.NextID = 9

	rule, err := w.services(t).Policies.AddRule(context.Background(), validRule())

	require.NoError(t, err)
	require.Equal(t, int64(9), rule.ID)
	insideTx(t, w.rec, "Policies.AddRule")
	insideTx(t, w.rec, "Policies.BumpVersion")
	insideTx(t, w.rec, "Events.Append")
	require.Equal(t, 1, w.rec.Count("Commit"))
	events := w.repo.Events.Appended()
	require.Len(t, events, 1)
	require.Equal(t, fleet.EventPolicyChanged, events[0].Kind)
	require.True(t, events[0].Kind.LogOnly(), "replicas poll policy_version")
	require.Equal(t, fleet.PolicyChangedPayload{Name: "role:edge-admins", Version: 5, Change: fleet.PolicyRuleCreated, ID: 9}, events[0].Payload)
}

func TestPolicyService_RejectsMalformedRule(t *testing.T) {
	for name, change := range map[string]func(r *fleet.PolicyRule){
		"subject without a kind": func(r *fleet.PolicyRule) { r.Subject = "alice" },
		"scope without a name":   func(r *fleet.PolicyRule) { r.Scope = "group" },
		"scope of unknown type":  func(r *fleet.PolicyRule) { r.Scope = "cluster:eu" },
		"unknown type":           func(r *fleet.PolicyRule) { r.Type = "cluster" },
		"unknown action":         func(r *fleet.PolicyRule) { r.Action = "fly" },
		"unknown effect":         func(r *fleet.PolicyRule) { r.Effect = "maybe" },
		"comma in a field":       func(r *fleet.PolicyRule) { r.Subject = "role:a,b" },
	} {
		t.Run(name, func(t *testing.T) {
			pending.Plan(t, "7.3", pendingPolicies)
			w := newWorld()
			rule := validRule()
			change(&rule)

			_, err := w.services(t).Policies.AddRule(context.Background(), rule)

			require.ErrorIs(t, err, fleet.ErrInvalid)
			require.Zero(t, w.rec.Count("Policies.AddRule"))
			require.Zero(t, w.rec.Count("Policies.BumpVersion"))
		})
	}

	t.Run("a binding to a user: role", func(t *testing.T) {
		pending.Plan(t, "7.3", pendingPolicies)
		w := newWorld()

		_, err := w.services(t).Policies.AddBinding(context.Background(), fleet.RoleBinding{Principal: "role:x", Role: "role:edge-admins"})

		require.ErrorIs(t, err, fleet.ErrInvalid)
		require.Zero(t, w.rec.Count("Policies.AddBinding"))
	})
}

// A scoped rule can never reach users, SPIRE, ops or the policy API.
func TestPolicyService_RejectsScopedRuleOnNonScopedType(t *testing.T) {
	for _, typ := range []string{"*", "user", "spire", "ops", "policy", "session"} {
		t.Run(typ, func(t *testing.T) {
			pending.Plan(t, "7.3", pendingPolicies)
			w := newWorld()
			rule := validRule()
			rule.Type = typ

			_, err := w.services(t).Policies.AddRule(context.Background(), rule)

			require.ErrorIs(t, err, fleet.ErrInvalid)
			require.Zero(t, w.rec.Count("Policies.AddRule"))
		})
	}
}
