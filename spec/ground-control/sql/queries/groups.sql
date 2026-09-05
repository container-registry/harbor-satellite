-- name: CreateGroup :one
INSERT INTO groups (group_name, registry_url, projects, created_at, updated_at)
VALUES ($1, $2, $3, NOW(), NOW())
  ON CONFLICT (group_name)
  DO UPDATE SET
  registry_url = EXCLUDED.registry_url,
  projects = EXCLUDED.projects,
  updated_at = NOW()
RETURNING *;

-- name: ListGroups :many
SELECT * FROM groups;

-- name: GetGroupByID :one
SELECT * FROM groups
WHERE id = $1;

-- name: GetGroupByName :one
SELECT * FROM groups
WHERE group_name = $1;

-- name: DeleteGroup :exec
DELETE FROM groups
WHERE id = $1;

-- name: GetProjectsOfGroup :many
SELECT projects FROM groups
WHERE group_name = $1;

-- name: CheckGroupExists :one
SELECT EXISTS(SELECT 1 FROM groups WHERE group_name = $1);
