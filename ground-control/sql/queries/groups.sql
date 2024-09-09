-- name: CreateGroup :one
INSERT INTO groups (group_name, parent_group_id, created_at, updated_at)
VALUES ($1, $2, $3, $4)
RETURNING *;

-- name: DeleteGroup :exec
DELETE FROM groups
WHERE id =  $1;

-- name: GetSubgroups :many
SELECT *
FROM groups
WHERE parent_group_id = $1;

-- name: ListGroups :many
SELECT * FROM groups;

-- name: GetGroupByID :one
SELECT * FROM groups
WHERE id = $1;

-- name: GetGroupByName :one
SELECT * FROM groups
WHERE group_name = $1;
