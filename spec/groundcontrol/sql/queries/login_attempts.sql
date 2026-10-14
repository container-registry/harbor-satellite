-- name: GetLoginAttempts :one
SELECT * FROM login_attempts
WHERE username = $1;

-- name: UpsertLoginAttempt :one
INSERT INTO login_attempts (username, failed_count, locked_until, last_attempt)
VALUES ($1, 1, NULL, NOW())
ON CONFLICT (username)
DO UPDATE SET
  failed_count = login_attempts.failed_count + 1,
  last_attempt = NOW()
RETURNING *;

-- name: ResetLoginAttempts :exec
UPDATE login_attempts
SET failed_count = 0, locked_until = NULL, last_attempt = NOW()
WHERE username = $1;

-- name: LockAccount :exec
UPDATE login_attempts
SET locked_until = $2, last_attempt = NOW()
WHERE username = $1;
