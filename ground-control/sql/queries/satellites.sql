-- name: CreateSatellite :one
INSERT INTO satellites (name, created_at, updated_at)
VALUES ($1, NOW(), NOW())
RETURNING *;

-- name: ListSatellites :many
SELECT * FROM satellites;

-- name: ListSatellitesByIDs :many
SELECT * FROM satellites
WHERE id = ANY($1::int[]);

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
