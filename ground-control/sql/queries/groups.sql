-- name: CreateGroup :one
INSERT INTO groups (group_name, created_at, updated_at)
VALUES ($1, $2, $3)
RETURNING *;

-- name: ListGroups :many
SELECT * FROM groups;

-- name: GetGroupByID :one
SELECT * FROM groups
WHERE id = $1;

-- name: GetGroupByName :one
SELECT * FROM groups
WHERE group_name = $1;
