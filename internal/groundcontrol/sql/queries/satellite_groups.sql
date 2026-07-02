-- name: AddSatelliteToGroup :exec
INSERT INTO satellite_groups (satellite_id, group_id)
VALUES ($1, $2)
ON CONFLICT DO NOTHING;

-- name: GroupSatelliteList :many
SELECT satellite_id, group_id FROM satellite_groups
WHERE group_id = $1;

-- name: SatelliteGroupList :many
SELECT satellite_id, group_id FROM satellite_groups
WHERE satellite_id = $1;

-- name: RemoveSatelliteFromGroup :exec
DELETE FROM satellite_groups
WHERE satellite_id = $1 AND group_id = $2;

-- name: CheckSatelliteInGroup :one
SELECT EXISTS (
    SELECT 1
    FROM satellite_groups
    WHERE satellite_id = $1 AND group_id = $2
);
