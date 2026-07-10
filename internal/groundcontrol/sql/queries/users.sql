-- name: CreateUser :one
INSERT INTO users (username, password_hash, role, created_at, updated_at)
VALUES ($1, $2, $3, NOW(), NOW())
RETURNING *;

-- name: GetUserByUsername :one
SELECT * FROM users
WHERE username = $1;

-- name: GetUserByID :one
SELECT * FROM users
WHERE id = $1;

-- name: ListUsers :many
SELECT id, username, role, created_at, updated_at FROM users
WHERE role != 'system_admin'
ORDER BY created_at DESC;

-- name: DeleteUser :exec
DELETE FROM users
WHERE username = $1 AND role != 'system_admin';

-- name: UpdateUserPassword :exec
UPDATE users
SET password_hash = $2, updated_at = NOW()
WHERE username = $1;

-- name: SystemAdminExists :one
SELECT EXISTS(SELECT 1 FROM users WHERE role = 'system_admin');
