package server

import (
	"net/http"
	"testing"

	"github.com/container-registry/harbor-satellite/internal/shared/testutil/pending"
	"github.com/stretchr/testify/require"
)

// appConfigWith is testAppConfig with one app_config key set.
func appConfigWith(t *testing.T, key string, value any) map[string]any {
	t.Helper()
	cfg := testAppConfig()
	app, ok := cfg["app_config"].(map[string]any)
	require.True(t, ok)
	app[key] = value
	return cfg
}

// invalidAppConfig fails pkg/config validation: a BYO registry needs a URL.
func invalidAppConfig(t *testing.T) map[string]any {
	t.Helper()
	return appConfigWith(t, "bring_own_registry", true)
}

func requireValidationError(t *testing.T, r response) {
	t.Helper()
	require.Equal(t, http.StatusUnprocessableEntity, r.Status, string(r.Body))
	var body ConfigValidationError
	r.decode(t, &body)
	require.Equal(t, http.StatusUnprocessableEntity, body.Code)
	require.Contains(t, body.Message, "custom registry URL is required")
}

func TestCreateConfig_InvalidReturns422NotStored(t *testing.T) {
	pending.Plan(t, "7.4", "configs are stored without validation")
	h := newGCHarness(t)

	r := h.do(http.MethodPost, "/api/configs", map[string]any{"config_name": "byo", "config": invalidAppConfig(t)}, h.admin)

	requireValidationError(t, r)
	h.mustDo(http.MethodGet, "/api/configs/byo", nil, h.admin, http.StatusNotFound)
	require.False(t, h.harbor.HasRepository("satellite/config-state/byo/state"), "nothing is pushed")
}

func TestUpdateConfig_InvalidPatchReturns422(t *testing.T) {
	pending.Plan(t, "7.4", "configs are stored without validation")
	h := newGCHarness(t)
	h.createConfig("edge-config")
	var before ConfigResponse
	h.mustDo(http.MethodGet, "/api/configs/edge-config", nil, h.admin, http.StatusOK).decode(t, &before)
	pushes := h.harbor.Mark()

	r := h.do(http.MethodPatch, "/api/configs/edge-config", map[string]any{"app_config": map[string]any{"bring_own_registry": true}}, h.admin)

	requireValidationError(t, r)
	var after ConfigResponse
	h.mustDo(http.MethodGet, "/api/configs/edge-config", nil, h.admin, http.StatusOK).decode(t, &after)
	require.Equal(t, before, after, "the merged document was not stored")
	require.Empty(t, h.harbor.CallsSince(pushes), "nothing is pushed")
}

// Warnings come back with the write; the document is stored as submitted.
func TestCreateConfig_WarningsInResponse(t *testing.T) {
	pending.Plan(t, "7.4", "configs are stored without validation")
	h := newGCHarness(t)
	cfg := appConfigWith(t, "log_level", "loud")

	r := h.mustDo(http.MethodPost, "/api/configs", map[string]any{"config_name": "loud", "config": cfg}, h.admin, http.StatusCreated)

	var created ConfigResponse
	r.decode(t, &created)
	require.NotEmpty(t, created.Warnings)
	require.Contains(t, created.Warnings[0], "log_level")
	var stored ConfigResponse
	h.mustDo(http.MethodGet, "/api/configs/loud", nil, h.admin, http.StatusOK).decode(t, &stored)
	require.Equal(t, "loud", stored.Config.AppConfig.LogLevel, "validation never rewrites the document")
	require.Empty(t, stored.Warnings, "warnings are part of the write's answer only")

	var patched ConfigResponse
	h.mustDo(http.MethodPatch, "/api/configs/loud", map[string]any{"app_config": map[string]any{"heartbeat_interval": "@every 00h01m00s"}}, h.admin, http.StatusOK).decode(t, &patched)
	require.NotEmpty(t, patched.Warnings, "an update answers the merged document's warnings")
}
