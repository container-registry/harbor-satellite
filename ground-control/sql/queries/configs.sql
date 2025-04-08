-- name: CreateConfig :one
INSERT INTO configs (config_name, registry_url, created_at, updated_at)
VALUES ($1, $2, NOW(), NOW())
  ON CONFLICT (config_name)
  DO UPDATE SET
    registry_url = EXCLUDED.registry_url,
    config = EXCLUDED.config,
    updated_at = NOW()
RETURNING *;

-- name: ListConfigs :many
SELECT * FROM configs;

-- name: GetConfigByID :one
SELECT * FROM configs
WHERE id = $1;

-- name: GetConfigByName :one
SELECT * FROM configs
WHERE config_name = $1;

-- name: DeleteConfig :exec
DELETE FROM configs
WHERE id = $1;

-- name: GetRawConfigByID :one
SELECT config FROM configs
WHERE id = $1;

-- name: GetRawConfigByName :one
SELECT config FROM configs
WHERE config_name = $1;
