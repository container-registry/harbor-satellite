-- name: CreateLabel :one
INSERT INTO labels (label_name, created_at, updated_at)
VALUES ($1, $2, $3)
RETURNING *;
