package config

import (
	"encoding/json"
	"testing"

	"github.com/container-registry/harbor-satellite/internal/shared/policybundle"
	"github.com/stretchr/testify/require"
)

// An app_config without admission keeps today's JSON, so config digests
// and stored documents do not change until an operator sets it.
func TestAppConfig_AdmissionOmittedWhenEmpty(t *testing.T) {
	data, err := json.Marshal(AppConfig{LogLevel: "info"})
	require.NoError(t, err)
	var unset map[string]any
	require.NoError(t, json.Unmarshal(data, &unset))
	require.NotContains(t, unset, "admission")

	var cfg AppConfig
	require.NoError(t, json.Unmarshal([]byte(`{
		"admission": {
			"mode": "enforce",
			"bundle": {"model_version": "edge-admission/v1", "policies": [["*", "library/blocked", "*", "*", "deny"]]}
		}
	}`), &cfg))
	want := AdmissionConfig{Mode: "enforce", Bundle: &policybundle.Bundle{
		ModelVersion: policybundle.ModelVersion,
		Policies:     [][]string{{"*", "library/blocked", "*", "*", "deny"}},
	}}
	require.Equal(t, want, cfg.Admission)

	cm := &ConfigManager{config: &Config{AppConfig: cfg}}
	require.Equal(t, want, cm.GetAdmission())
}
