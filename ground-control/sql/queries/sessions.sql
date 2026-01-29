-- name: CreateSession :one
INSERT INTO sessions (user_id, token, expires_at, created_at)
VALUES ($1, $2, $3, NOW())
RETURNING *;

-- name: GetSessionByToken :one
SELECT s.*, u.username, u.role
FROM sessions s
JOIN users u ON s.user_id = u.id
WHERE s.token = $1 AND s.expires_at > NOW();

-- name: DeleteSession :exec
DELETE FROM sessions
WHERE token = $1;

-- name: DeleteUserSessions :exec
DELETE FROM sessions
WHERE user_id = $1;

-- name: DeleteExpiredSessions :exec
DELETE FROM sessions
WHERE expires_at <= NOW();
