package satconfig_test

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/container-registry/harbor-satellite/internal/groundcontrol/satconfig"
	"github.com/container-registry/harbor-satellite/internal/shared/testutil/pending"
	"github.com/container-registry/harbor-satellite/pkg/config"
	"github.com/stretchr/testify/require"
)

// The validator adds no rules of its own: it answers what
// config.ValidateAppConfig answers for the document's app_config.
func TestValidator_DelegatesToPkgConfig(t *testing.T) {
	pending.Plan(t, "7.4", "satconfig.Validator is a stub")
	ctx := context.Background()
	for name, tc := range map[string]struct {
		doc          string
		warns        bool
		errSubstring string
	}{
		"valid":             {doc: `{"app_config":{"log_level":"info","state_replication_interval":"@every 00h00m30s","register_satellite_interval":"@every 00h00m05s","heartbeat_interval":"@every 00h00m30s"}}`},
		"warning":           {doc: `{"app_config":{"log_level":"loud","state_replication_interval":"@every 00h00m30s","register_satellite_interval":"@every 00h00m05s","heartbeat_interval":"@every 00h00m30s"}}`, warns: true},
		"error":             {doc: `{"app_config":{"bring_own_registry":true}}`, errSubstring: "custom registry URL is required"},
		"no ground control": {doc: `{"app_config":{"state_replication_interval":"@every 00h00m30s"},"state_config":{}}`, warns: true},
	} {
		var cfg config.Config
		require.NoError(t, json.Unmarshal([]byte(tc.doc), &cfg), name)
		wantWarnings, wantErr := config.ValidateAppConfig(cfg.AppConfig)

		warnings, err := satconfig.Validator{}.ValidateConfig(ctx, json.RawMessage(tc.doc))

		if tc.errSubstring != "" {
			require.ErrorContains(t, err, tc.errSubstring, name)
			require.EqualError(t, err, wantErr.Error(), name)
			continue
		}
		require.NoError(t, err, name)
		require.Equal(t, tc.warns, len(warnings) > 0, "%s: %v", name, warnings)
		require.Equal(t, wantWarnings, warnings, name)
	}

	_, err := satconfig.Validator{}.ValidateConfig(ctx, json.RawMessage(`{"app_config":`))
	require.Error(t, err, "a document that does not decode")
}
