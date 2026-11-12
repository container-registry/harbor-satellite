-- xid8 values cross the driver as text.

-- name: CreateWebhookSubscription :one
-- The checkpoint starts at the current (cluster-wide) xmin: events of
-- transactions that finished before it are skipped; events of transactions
-- at or above it are delivered, including ones that committed before the
-- subscription while an older transaction was still open.
WITH sub AS (
    INSERT INTO webhook_subscriptions (url, secret_ciphertext, event_types, enabled, created_by)
    VALUES (@url, @secret_ciphertext, @event_types::TEXT[], TRUE, sqlc.narg(created_by))
    RETURNING id, url, event_types, enabled, disabled_reason, created_at
), cp AS (
    INSERT INTO webhook_checkpoints (subscription_id, last_xid, last_id)
    SELECT id, pg_snapshot_xmin(pg_current_snapshot()), 0 FROM sub
)
SELECT id, url, event_types, enabled, disabled_reason, created_at FROM sub;

-- name: ListWebhookSubscriptions :many
SELECT s.id, s.url, s.event_types, s.enabled, s.disabled_reason, s.created_at, c.attempts, c.last_error
FROM webhook_subscriptions s
JOIN webhook_checkpoints c ON c.subscription_id = s.id
ORDER BY s.id;

-- name: DeleteWebhookSubscription :execrows
DELETE FROM webhook_subscriptions WHERE id = @id;

-- name: ListDueWebhooks :many
SELECT s.id, s.url, s.secret_ciphertext, s.event_types, c.last_xid::TEXT AS last_xid, c.last_id, c.attempts
FROM webhook_subscriptions s
JOIN webhook_checkpoints c ON c.subscription_id = s.id
WHERE s.enabled AND c.next_attempt_at <= NOW()
ORDER BY c.next_attempt_at, s.id
LIMIT @max_rows;

-- name: AdvanceWebhookCheckpoint :exec
-- Never moves back; a delivery ends the failure streak.
UPDATE webhook_checkpoints
SET last_xid = sqlc.arg(last_xid)::TEXT::XID8, last_id = sqlc.arg(last_id)::BIGINT, attempts = 0, failing_since = NULL,
    last_status = sqlc.narg(last_status), last_error = NULL, next_attempt_at = NOW(), updated_at = NOW()
WHERE subscription_id = sqlc.arg(subscription_id)
  AND (last_xid, last_id) < (sqlc.arg(last_xid)::TEXT::XID8, sqlc.arg(last_id)::BIGINT);

-- name: FailWebhookDelivery :one
-- Retry delay as an interval on the database clock. A subscription failing
-- without a single delivery for longer than disable_after is disabled, so
-- a dead receiver cannot hold back retention.
WITH cp AS (
    UPDATE webhook_checkpoints
    SET attempts = attempts + 1, failing_since = COALESCE(failing_since, NOW()),
        last_status = sqlc.narg(last_status), last_error = sqlc.arg(last_error)::TEXT,
        next_attempt_at = NOW() + sqlc.arg(retry_in_ms)::BIGINT * INTERVAL '1 millisecond', updated_at = NOW()
    WHERE subscription_id = sqlc.arg(subscription_id)
    RETURNING attempts, failing_since
), off AS (
    UPDATE webhook_subscriptions SET enabled = FALSE,
        disabled_reason = 'failing since ' || (SELECT failing_since FROM cp)::TEXT || ': ' || sqlc.arg(last_error)::TEXT
    WHERE id = sqlc.arg(subscription_id)
      AND (SELECT failing_since FROM cp) <= NOW() - sqlc.arg(disable_after_ms)::BIGINT * INTERVAL '1 millisecond'
    RETURNING id
)
-- attempts is 0 when the subscription was deleted meanwhile.
SELECT COALESCE((SELECT attempts FROM cp), 0)::INT AS attempts, EXISTS (SELECT 1 FROM off) AS disabled;
