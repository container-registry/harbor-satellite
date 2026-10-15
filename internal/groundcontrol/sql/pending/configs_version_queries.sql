-- Plan item 3.4. Not compiled: sqlc reads sql/queries only. Moves to
-- sql/queries/configs.sql together with constraints.sql.

-- name: UpdateConfigIfVersion :one
UPDATE configs SET config = $3, updated_at = NOW(), version = version + 1
WHERE config_name = $1 AND version = $2
RETURNING *;
