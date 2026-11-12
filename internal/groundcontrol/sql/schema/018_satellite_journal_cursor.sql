-- +goose Up
-- How far Ground Control holds each satellite's edge journal (plan item
-- 6.5, ADR-0015). Accepted records become log-only rows of event_outbox,
-- which is their only store, so the cursor alone deduplicates uploads.
CREATE TABLE satellite_journal_cursor (
    satellite_id INT PRIMARY KEY REFERENCES satellites(id) ON DELETE CASCADE,
    epoch        BIGINT NOT NULL CHECK (epoch > 0),
    acked_seq    BIGINT NOT NULL DEFAULT 0 CHECK (acked_seq >= 0)
);

-- +goose Down
DROP TABLE satellite_journal_cursor;
