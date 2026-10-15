-- Session-level locks: the caller runs these on one dedicated connection
-- and keeps no transaction open while it holds the lock.

-- name: TryAggregateLock :one
SELECT pg_try_advisory_lock(sqlc.arg(namespace)::INT, sqlc.arg(id)::INT) AS acquired;

-- name: AggregateUnlock :one
SELECT pg_advisory_unlock(sqlc.arg(namespace)::INT, sqlc.arg(id)::INT) AS released;
