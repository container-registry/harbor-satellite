-- name: CreateConfig :one
INSERT INTO configs (config_name, registry_url, config, created_at, updated_at)
VALUES ($1, $2, $3, NOW(), NOW())
ON CONFLICT (config_name)
DO UPDATE SET
  registry_url = EXCLUDED.registry_url,
  config = EXCLUDED.config,
  updated_at = NOW()
RETURNING id, config_name, registry_url, config, created_at, updated_at;

-- name: ListConfigs :many
SELECT id, config_name, registry_url, config, created_at, updated_at FROM configs;

-- name: GetConfigByID :one
SELECT id, config_name, registry_url, config, created_at, updated_at FROM configs
WHERE id = $1;

-- name: GetConfigByName :one
SELECT id, config_name, registry_url, config, created_at, updated_at FROM configs
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
