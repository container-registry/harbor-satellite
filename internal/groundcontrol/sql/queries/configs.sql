-- name: CreateConfig :one
INSERT INTO configs (config_name, registry_url, config, created_at, updated_at)
VALUES ($1, $2, $3, NOW(), NOW())
RETURNING id, config_name, registry_url, config, created_at, updated_at;

-- name: UpdateConfig :one
UPDATE configs
SET registry_url = $2,
    config = $3,
    updated_at = NOW()
WHERE config_name = $1
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


-- name: GetConfigWithVersionByName :one
SELECT id, config_name, registry_url, config, version FROM configs
WHERE config_name = $1;

-- name: UpdateConfigIfVersion :one
UPDATE configs
SET registry_url = $3,
    config = $4,
    updated_at = NOW(),
    version = version + 1
WHERE config_name = $1 AND version = $2
RETURNING id, config_name, registry_url, config, created_at, updated_at;

-- Adapter-only queries (plan item 1.3): whole rows, version included, for
-- the optimistic update of plan item 3.4.

-- name: CreateFleetConfig :one
INSERT INTO configs (config_name, registry_url, config, created_at, updated_at)
VALUES ($1, $2, $3, NOW(), NOW())
RETURNING *;

-- name: GetFleetConfigByID :one
SELECT * FROM configs WHERE id = $1;

-- name: GetFleetConfigByName :one
SELECT * FROM configs WHERE config_name = $1;

-- name: ListFleetConfigs :many
SELECT * FROM configs ORDER BY id;

-- name: UpdateFleetConfig :one
UPDATE configs
SET registry_url = $3,
    config = $4,
    updated_at = NOW(),
    version = version + 1
WHERE id = $1 AND version = $2
RETURNING *;

-- name: ConfigInUse :one
SELECT EXISTS (SELECT 1 FROM satellite_configs WHERE config_id = $1);
