-- name: SetSatelliteConfig :exec
INSERT INTO groundcontrol.satellite_configs (satellite_id, config_id)
VALUES ($1, $2)
ON CONFLICT (satellite_id)
DO UPDATE SET config_id = EXCLUDED.config_id;

-- name: ConfigSatelliteList :many
SELECT * FROM groundcontrol.satellite_configs
WHERE config_id = $1;

-- name: SatelliteConfig :one
SELECT (satellite_id, config_id) FROM groundcontrol.satellite_configs
WHERE satellite_id = $1;

-- name: RemoveSatelliteFromConfig :exec
DELETE FROM groundcontrol.satellite_configs
WHERE satellite_id = $1 AND config_id = $2;


