-- Indexes for two lookups that otherwise scan their table:
-- ListGroupsTrackingTag, run for every registry push, matches artifacts
-- with @>, and ListStuckOutboxEvents lists pending events oldest first.
-- CONCURRENTLY keeps API writes going while an existing table is indexed;
-- it cannot run in a transaction. A build that failed leaves an invalid
-- index behind, so Up drops it before building again.

-- +goose NO TRANSACTION

-- +goose Up
DROP INDEX CONCURRENTLY IF EXISTS idx_group_artifacts_artifacts;
CREATE INDEX CONCURRENTLY idx_group_artifacts_artifacts ON group_artifacts USING GIN (artifacts jsonb_path_ops);
DROP INDEX CONCURRENTLY IF EXISTS idx_event_outbox_pending_created;
CREATE INDEX CONCURRENTLY idx_event_outbox_pending_created ON event_outbox (created_at, id) WHERE done_at IS NULL;

-- +goose Down
DROP INDEX CONCURRENTLY IF EXISTS idx_event_outbox_pending_created;
DROP INDEX CONCURRENTLY IF EXISTS idx_group_artifacts_artifacts;
