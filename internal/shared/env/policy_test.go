package env

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestPolicy_Validate(t *testing.T) {
	require.NoError(t, Policy{Engine: PolicyEngineBuiltin, PollInterval: time.Second}.Validate())
	require.NoError(t, Policy{Engine: PolicyEngineCasbin, PollInterval: time.Second}.Validate())
	require.ErrorContains(t, Policy{Engine: "casbn", PollInterval: time.Second}.Validate(), "POLICY_ENGINE")
	require.ErrorContains(t, Policy{Engine: PolicyEngineBuiltin}.Validate(), "POLICY_POLL_INTERVAL")
}
