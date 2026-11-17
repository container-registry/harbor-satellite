package server

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"

	"github.com/container-registry/harbor-satellite/internal/groundcontrol/adapters/casbin"
	"github.com/container-registry/harbor-satellite/internal/groundcontrol/adapters/postgres"
	"github.com/container-registry/harbor-satellite/internal/groundcontrol/fleet"
	"github.com/stretchr/testify/require"
)

// The policy API stores rules and bindings and bumps the version, so a
// Casbin policy over the same database reloads them.
func TestPolicyAPI_StoresRulesAndBindingsAndBumpsVersion(t *testing.T) {
	h := newGCHarness(t)

	var rule PolicyRule
	h.mustDo(http.MethodPost, "/api/policies", PolicyRuleRequest{Subject: "role:group-admin:A", Scope: "group:A", ResourceType: "group", Effect: "allow"}, h.admin, http.StatusCreated).decode(t, &rule)
	require.Equal(t, "*", rule.Action, "the action defaults to *")
	h.mustDo(http.MethodPost, "/api/policies", PolicyRuleRequest{Subject: "role:x", Scope: "group:A", ResourceType: "user", Effect: "allow"}, h.admin, http.StatusBadRequest)
	var binding RoleBinding
	h.mustDo(http.MethodPost, "/api/role-bindings", RoleBindingRequest{Principal: "user:bob", Role: "role:group-admin:A"}, h.admin, http.StatusCreated).decode(t, &binding)
	h.mustDo(http.MethodPost, "/api/role-bindings", RoleBindingRequest{Principal: "user:bob", Role: "role:admin"}, h.admin, http.StatusBadRequest)

	var rules []PolicyRule
	h.mustDo(http.MethodGet, "/api/policies", nil, h.admin, http.StatusOK).decode(t, &rules)
	require.Equal(t, []PolicyRule{rule}, rules)

	repo, err := postgres.NewRepository(h.db.DB)
	require.NoError(t, err)
	p, err := casbin.New(context.Background(), repo.Autocommit().Policies(), time.Hour)
	require.NoError(t, err)
	d, err := p.Authorize(context.Background(), fleet.Principal{Subject: "user:bob"}, fleet.ActionSync, fleet.Resource{Type: fleet.ResourceGroup, ID: "A"})
	require.NoError(t, err)
	require.True(t, d.Allowed, "the stored rule and binding load")

	h.mustDo(http.MethodDelete, "/api/role-bindings/"+strconv.FormatInt(binding.ID, 10), nil, h.admin, http.StatusNoContent)
	h.mustDo(http.MethodDelete, "/api/policies/"+strconv.FormatInt(rule.ID, 10), nil, h.admin, http.StatusNoContent)
	h.mustDo(http.MethodDelete, "/api/policies/"+strconv.FormatInt(rule.ID, 10), nil, h.admin, http.StatusNotFound)
	require.NoError(t, p.Reload(context.Background()))
	d, err = p.Authorize(context.Background(), fleet.Principal{Subject: "user:bob"}, fleet.ActionSync, fleet.Resource{Type: fleet.ResourceGroup, ID: "A"})
	require.NoError(t, err)
	require.False(t, d.Allowed, "the reload sees the deletes")
}

func TestPolicyAndWebhookErrors_HideInternalDetails(t *testing.T) {
	for name, answer := range map[string]func(http.ResponseWriter, string, error){"policy": policyError, "webhook": webhookError} {
		t.Run(name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			answer(rec, "list", errors.New(`ERROR: relation "secret_table" does not exist`))
			require.Equal(t, http.StatusInternalServerError, rec.Code)
			require.NotContains(t, rec.Body.String(), "secret_table")

			rec = httptest.NewRecorder()
			answer(rec, "create", fmt.Errorf("%w: unknown resource type", fleet.ErrInvalid))
			require.Equal(t, http.StatusBadRequest, rec.Code)
			require.Contains(t, rec.Body.String(), "unknown resource type", "a mapped error keeps its message")
		})
	}
}
