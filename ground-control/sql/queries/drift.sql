-- name: UpsertConfigDigest :exec
INSERT INTO config_digests (config_id, digest, updated_at)
VALUES ($1, $2, NOW())
ON CONFLICT (config_id)
DO UPDATE SET digest = EXCLUDED.digest, updated_at = NOW();

-- name: GetConfigDigest :one
SELECT config_id, digest, updated_at FROM config_digests
WHERE config_id = $1;

-- name: UpsertSatelliteDesiredState :exec
INSERT INTO satellite_desired_states (
    satellite_id, expected_state_digest, expected_config_digest, updated_at
)
VALUES ($1, $2, $3, NOW())
ON CONFLICT (satellite_id)
DO UPDATE SET
    expected_state_digest = EXCLUDED.expected_state_digest,
    expected_config_digest = EXCLUDED.expected_config_digest,
    updated_at = NOW();

-- name: GetSatelliteDesiredState :one
SELECT satellite_id, expected_state_digest, expected_config_digest, last_converged_at, updated_at
FROM satellite_desired_states
WHERE satellite_id = $1;

-- name: UpdateSatelliteDesiredConfigDigestForConfig :exec
INSERT INTO satellite_desired_states (satellite_id, expected_config_digest, updated_at)
SELECT sc.satellite_id, $2, NOW()
FROM satellite_configs sc
WHERE sc.config_id = $1
ON CONFLICT (satellite_id)
DO UPDATE SET
    expected_config_digest = EXCLUDED.expected_config_digest,
    updated_at = NOW();

-- name: UpdateSatelliteLastConvergedAt :exec
UPDATE satellite_desired_states
SET last_converged_at = $2,
    updated_at = NOW()
WHERE satellite_id = $1;
