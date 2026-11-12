-- name: AppendOutboxEvent :one
-- A log-only event is inserted already done: readers such as webhooks see
-- it, the publisher never claims it.
INSERT INTO event_outbox (kind, aggregate_type, aggregate_id, payload, done_at)
VALUES (
    sqlc.arg(kind), sqlc.arg(aggregate_type), sqlc.arg(aggregate_id), sqlc.arg(payload),
    CASE WHEN sqlc.arg(log_only)::BOOLEAN THEN NOW() END
)
RETURNING id;

-- name: AppendGroupMemberEvents :execrows
-- One event per member of the group, in one statement however large the
-- group is.
INSERT INTO event_outbox (kind, aggregate_type, aggregate_id, payload)
SELECT sqlc.arg(kind)::TEXT, 'satellite', sg.satellite_id, jsonb_build_object('name', s.name)
FROM satellite_groups sg
JOIN satellites s ON s.id = sg.satellite_id
WHERE sg.group_id = sqlc.arg(group_id)::INT;

-- name: ClaimOutboxEvents :many
-- Leases up to batch_size due events for lease_ms. attempts counts claims,
-- so an event whose handler kills the process still reaches max_attempts.
-- The CTE is materialized so the LIMIT and the row locks are evaluated
-- once. Every time here is the database's, so a worker's clock cannot
-- shorten a lease.
WITH due AS MATERIALIZED (
    SELECT id FROM event_outbox
    WHERE done_at IS NULL
      AND next_attempt_at <= NOW()
      AND attempts < sqlc.arg(max_attempts)::INT
    ORDER BY id
    LIMIT sqlc.arg(batch_size)::INT
    FOR UPDATE SKIP LOCKED
)
UPDATE event_outbox AS o
SET attempts = o.attempts + 1,
    next_attempt_at = NOW() + sqlc.arg(lease_ms)::BIGINT * INTERVAL '1 millisecond'
FROM due
WHERE o.id = due.id
RETURNING o.id, o.kind, o.aggregate_type, o.aggregate_id, o.payload, o.attempts, o.created_at;

-- name: CompleteOutboxEvents :exec
UPDATE event_outbox
SET done_at = NOW(), last_error = NULL
WHERE id = ANY(sqlc.arg(ids)::BIGINT[]);

-- name: FailOutboxEvent :exec
UPDATE event_outbox
SET last_error = sqlc.arg(last_error)::TEXT,
    next_attempt_at = NOW() + sqlc.arg(retry_in_ms)::BIGINT * INTERVAL '1 millisecond'
WHERE id = sqlc.arg(id) AND done_at IS NULL;

-- name: ReleaseOutboxEvent :exec
-- Gives the claim's attempt back: the event could not be handled for a
-- reason that is not its own (registry unreachable, aggregate locked).
UPDATE event_outbox
SET attempts = GREATEST(attempts - 1, 0),
    next_attempt_at = NOW() + sqlc.arg(retry_in_ms)::BIGINT * INTERVAL '1 millisecond',
    last_error = NULLIF(sqlc.arg(last_error)::TEXT, '')
WHERE id = sqlc.arg(id) AND done_at IS NULL;

-- name: RearmOutboxEvents :execrows
-- Makes every event that ran out of attempts due again, after the
-- registry came back.
UPDATE event_outbox
SET attempts = 0, next_attempt_at = NOW()
WHERE done_at IS NULL AND attempts >= sqlc.arg(max_attempts)::INT;

-- name: ListStuckOutboxEvents :many
-- min_attempts 0 lists every pending event.
SELECT id, kind, aggregate_type, aggregate_id, attempts, created_at, next_attempt_at, last_error
FROM event_outbox
WHERE done_at IS NULL AND attempts >= sqlc.arg(min_attempts)::INT
ORDER BY id
LIMIT sqlc.arg(max_rows)::INT;

-- name: CountPendingOutboxEvents :one
SELECT count(*) FROM event_outbox WHERE done_at IS NULL;
