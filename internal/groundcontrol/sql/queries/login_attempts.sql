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

-- now is a parameter because locked_until is written by the application as
-- TIMESTAMP; NOW() would compare it with the server's wall clock.
-- name: DeleteStaleLoginAttempts :exec
DELETE FROM login_attempts
WHERE last_attempt < sqlc.arg(now)::TIMESTAMP - make_interval(days => sqlc.arg(days)::INT)
  AND (locked_until IS NULL OR locked_until < sqlc.arg(now)::TIMESTAMP);
