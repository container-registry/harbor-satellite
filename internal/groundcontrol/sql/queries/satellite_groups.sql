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

-- name: GetProjectsOfSatellites :many
SELECT ra.satellite_id,
       ra.robot_id,
       ra.robot_name,
       COALESCE(
         array_agg(DISTINCT p.project ORDER BY p.project) FILTER (WHERE p.project IS NOT NULL),
         '{}'
       )::TEXT[] AS projects
FROM robot_accounts ra
LEFT JOIN satellite_groups sg ON sg.satellite_id = ra.satellite_id
LEFT JOIN groups g ON g.id = sg.group_id
LEFT JOIN LATERAL unnest(g.projects) AS p(project) ON TRUE
WHERE ra.satellite_id = ANY(sqlc.arg(satellite_ids)::INT[])
GROUP BY ra.satellite_id, ra.robot_id, ra.robot_name
ORDER BY ra.satellite_id, ra.robot_id;

-- name: ListGroupNamesOfSatellites :many
-- The parent scopes of a set of satellites in one statement (plan item
-- 7.3): what a service loads before it authorizes, and what filters a list
-- without a query per item.
SELECT sg.satellite_id, g.group_name
FROM satellite_groups sg
JOIN groups g ON g.id = sg.group_id
WHERE sg.satellite_id = ANY(sqlc.arg(satellite_ids)::INT[])
ORDER BY sg.satellite_id, g.group_name;
