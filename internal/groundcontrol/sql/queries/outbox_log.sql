-- name: ReadOutboxAfter :many
-- Rows of transactions still in flight are hidden by the xmin guard, so a
-- checkpoint never passes a row that commits later with a lower xid. The
-- guard is cluster-wide: any open transaction on the server holds delivery
-- back. ORDER BY names the column, because the text alias sorts 10 before 9.
SELECT xid::TEXT AS xid, id, kind, aggregate_type, aggregate_id, payload, created_at
FROM event_outbox
WHERE (event_outbox.xid, event_outbox.id) > (sqlc.arg(after_xid)::TEXT::XID8, sqlc.arg(after_id)::BIGINT)
  AND event_outbox.xid < pg_snapshot_xmin(pg_current_snapshot())
ORDER BY event_outbox.xid, event_outbox.id
LIMIT sqlc.arg(max_rows)::INT;

-- name: PruneDoneOutboxEvents :execrows
-- Keeps every row above the lowest checkpoint of an enabled subscription.
DELETE FROM event_outbox o
WHERE o.id IN (
    SELECT e.id FROM event_outbox e
    WHERE e.done_at IS NOT NULL AND e.done_at < NOW() - sqlc.arg(retention_ms)::BIGINT * INTERVAL '1 millisecond'
      AND NOT EXISTS (SELECT 1 FROM webhook_checkpoints c JOIN webhook_subscriptions s ON s.id = c.subscription_id
                      WHERE s.enabled AND (e.xid, e.id) > (c.last_xid, c.last_id))
    ORDER BY e.id LIMIT sqlc.arg(max_rows)::INT);
