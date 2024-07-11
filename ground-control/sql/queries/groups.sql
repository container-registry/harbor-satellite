-- name: CreateGroup :one
INSERT INTO groups (id, group_name, username, password, created_at, updated_at)
VALUES ($1, $2, $3, $4, $5, $6)
RETURNING *;

-- name: GetGroups :many
SELECT * FROM groups;
