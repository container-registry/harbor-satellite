-- name: AddSatelliteToGroup :exec
INSERT INTO satellite_groups (satellite_id, group_id)
VALUES ($1, $2)
ON CONFLICT DO NOTHING;
