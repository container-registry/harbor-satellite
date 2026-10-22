-- name: CreateSatellite :one
INSERT INTO satellites (name, created_at, updated_at)
VALUES ($1, NOW(), NOW())
RETURNING *;

-- name: ListSatellites :many
SELECT * FROM satellites;

-- name: GetSatellitesByGroupName :many
SELECT s.id, s.name, s.created_at, s.updated_at
FROM satellites s
JOIN satellite_groups sg ON sg.satellite_id = s.id
JOIN groups g ON g.id = sg.group_id
WHERE g.group_name = $1;

-- name: GetSatellite :one
SELECT * FROM satellites
WHERE id = $1 LIMIT 1;

-- name: GetSatelliteByName :one
SELECT * FROM satellites
WHERE name = $1 LIMIT 1;

-- -- name: GetSatelliteByToken :one
-- SELECT * FROM satellites
-- WHERE token = $1 LIMIT 1;

-- name: GetSatelliteID :one
SELECT id FROM satellites
WHERE name = $1 LIMIT 1;

-- name: DeleteSatelliteByName :exec
DELETE FROM satellites
WHERE name = $1;

-- name: DeleteSatellite :exec
DELETE FROM satellites
WHERE id = $1;
