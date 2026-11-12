-- name: OpenJournalCursor :one
-- Returns the satellite's cursor after creating it or moving it to a newer
-- epoch with acked_seq 0 (a reinstalled satellite), and whether it did
-- either (fresh). Epochs are time-ordered: an older epoch leaves the cursor,
-- so a delayed upload of a replaced journal cannot move it back. The
-- statement always returns the row and locks it until the transaction ends,
-- so two uploads of one satellite run one after the other. prior reads the
-- cursor under that lock, so it sees the latest committed epoch; the INSERT
-- reads prior first, because a row this statement has updated is no longer
-- visible to it.
WITH prior AS MATERIALIZED (
    SELECT c.epoch FROM satellite_journal_cursor c
    WHERE c.satellite_id = sqlc.arg(satellite_id)::INT
    FOR UPDATE
)
INSERT INTO satellite_journal_cursor (satellite_id, epoch, acked_seq)
SELECT sqlc.arg(satellite_id)::INT, sqlc.arg(epoch)::BIGINT, 0
FROM (SELECT count(*) FROM prior) AS read_prior
ON CONFLICT (satellite_id) DO UPDATE
SET epoch = GREATEST(satellite_journal_cursor.epoch, EXCLUDED.epoch),
    acked_seq = CASE
        WHEN EXCLUDED.epoch > satellite_journal_cursor.epoch THEN 0
        ELSE satellite_journal_cursor.acked_seq
    END
RETURNING epoch, acked_seq,
    NOT EXISTS (SELECT 1 FROM prior WHERE prior.epoch >= sqlc.arg(epoch)::BIGINT) AS fresh;

-- name: AdvanceJournalCursor :one
-- One statement per accepted upload (A99): moves the cursor of epoch forward
-- to acked_seq and, only when it moved, appends the upload's edge events to
-- the outbox, log-only (already done) and in the given order. events is a
-- JSON array of {kind, payload}.
WITH moved AS (
    UPDATE satellite_journal_cursor SET acked_seq = sqlc.arg(acked_seq)::BIGINT
    WHERE satellite_id = sqlc.arg(satellite_id)::INT
      AND epoch = sqlc.arg(epoch)::BIGINT
      AND acked_seq < sqlc.arg(acked_seq)::BIGINT
    RETURNING satellite_id
), emitted AS (
    INSERT INTO event_outbox (kind, aggregate_type, aggregate_id, payload, done_at)
    SELECT e.item->>'kind', 'edge', m.satellite_id, e.item->'payload', NOW()
    FROM moved m
    CROSS JOIN jsonb_array_elements(sqlc.arg(events)::JSONB) WITH ORDINALITY AS e(item, ord)
    ORDER BY e.ord
    RETURNING 1
)
SELECT (SELECT count(*) FROM moved)::BIGINT AS moved, (SELECT count(*) FROM emitted)::BIGINT AS emitted;
