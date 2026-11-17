package dbtest

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// pg_stat_statements renders LIMIT 1 and LIMIT $2 alike, so the two status
// queries share one text and neither name alone is right.
func TestStatementKey_NamesEveryQueryOfAnAmbiguousText(t *testing.T) {
	const recorded = `SELECT id, satellite_id, activity, latest_state_digest, latest_config_digest, cpu_percent, memory_used_bytes, storage_used_bytes, last_sync_duration_ms, image_count, reported_at, created_at, artifact_ids FROM satellite_status
WHERE satellite_id = $1 ORDER BY created_at DESC LIMIT $2`

	require.Equal(t, "GetLatestSatelliteStatus|GetSatelliteStatusHistory", statementKey(recorded))
	require.Equal(t, "DeleteExpiredOIDCLoginStates", statementKey("DELETE FROM oidc_login_states WHERE expires_at <= $1::TIMESTAMPTZ"))
}
