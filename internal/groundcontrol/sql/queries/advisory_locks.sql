-- name: TryAdvisoryXactLock :one
-- The lock is released when the transaction ends, so it cannot outlive the
-- connection it was taken on.
SELECT pg_try_advisory_xact_lock(sqlc.arg(lock_id)::BIGINT) AS acquired;
