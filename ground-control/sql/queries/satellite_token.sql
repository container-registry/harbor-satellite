-- name: AddToken :one
INSERT INTO satellite_token (satellite_id, token, created_at, updated_at)
VALUES ($1, $2, NOW(), NOW())
RETURNING token;

-- name: GetSatelliteIDByToken :one
SELECT satellite_id
FROM satellite_token
WHERE token = $1;

-- name: GetToken :one
SELECT * FROM satellite_token
WHERE id = $1;

-- name: ListToken :many
SELECT * FROM satellite_token;

-- name: DeleteToken :exec
DELETE FROM satellite_token
WHERE token = $1;
