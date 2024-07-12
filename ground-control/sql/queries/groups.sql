-- name: CreateGroup :one
INSERT INTO groups (id, group_name, username, password, created_at, updated_at)
VALUES ($1, $2, $3, $4, $5, $6)
RETURNING *;

-- name: ListGroups :many
SELECT * FROM groups;

-- name: GetGroup :one
SELECT * FROM groups
WHERE group_name = $1 LIMIT 1;

-- name: GetGroupID :one
SELECT id FROM groups
WHERE group_name = $1 LIMIT 1;

-- name: DeleteGroupByName :exec
DELETE FROM groups
WHERE group_name = $1;

-- name: DeleteGroupByID :exec
DELETE FROM groups
WHERE id = $1;

-- name: Authenticate :one
SELECT id FROM groups
WHERE username = $1 AND password = $2 AND group_name = $3;
