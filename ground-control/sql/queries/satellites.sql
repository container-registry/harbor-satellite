-- name: CreateSatellite :one
INSERT INTO satellites (name, token, created_at, updated_at)
VALUES ($1, $2, $3, $4)
RETURNING *;

-- name: ListSatellites :many
SELECT * FROM satellites;

-- name: GetSatelliteByName :one
SELECT * FROM satellites
WHERE name = $1 LIMIT 1;

-- name: GetSatelliteByToken :one
SELECT * FROM satellites
WHERE token = $1 LIMIT 1;

-- name: GetSatelliteID :one
SELECT id FROM satellites
WHERE name = $1 LIMIT 1;

-- name: DeleteSatellite :exec
DELETE FROM satellites
WHERE id = $1;
