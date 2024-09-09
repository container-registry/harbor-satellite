-- name: AddSatelliteToGroup :exec
INSERT INTO satellite_groups (satellite_id, group_id)
VALUES ($1, $2)
  ON CONFLICT DO NOTHING;

-- name: RemoveSatelliteFromGroup :exec
DELETE FROM satellite_groups
WHERE satellite_id = $1 AND group_id = $2;
