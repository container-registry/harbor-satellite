-- +goose Up

-- Events are appended in the transaction that changes the fleet and
-- handled by the async plane (ADR-0014). xid is the appending
-- transaction's ID; ordered readers (webhooks) need it, the work queue
-- does not. xid8 and pg_current_xact_id() need PostgreSQL 13; ADR-0020
-- sets the minimum to 19. Log-only kinds are inserted already done.
CREATE TABLE event_outbox (
    id              BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    xid             XID8 NOT NULL DEFAULT pg_current_xact_id(),
    kind            TEXT NOT NULL,
    aggregate_type  TEXT NOT NULL,
    aggregate_id    BIGINT NOT NULL,
    payload         JSONB NOT NULL DEFAULT '{}'::JSONB,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    attempts        INT NOT NULL DEFAULT 0,
    next_attempt_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    last_error      TEXT NULL,
    done_at         TIMESTAMPTZ NULL
);

CREATE INDEX idx_event_outbox_due ON event_outbox (next_attempt_at) WHERE done_at IS NULL;
CREATE INDEX idx_event_outbox_aggregate ON event_outbox (aggregate_type, aggregate_id) WHERE done_at IS NULL;

-- The artifacts of a group, written by the API transaction; groups keeps
-- its columns so the JSON of the group endpoints does not change.
CREATE TABLE group_artifacts (
    group_id   INT PRIMARY KEY REFERENCES groups(id) ON DELETE CASCADE,
    artifacts  JSONB NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- What GC last rendered and published per aggregate, derived from the
-- tables above; losing a row costs one push, never data. document is the
-- rendered desired state, published_artifact_digest the manifest digest
-- the registry returned, which satellites report back as applied.
CREATE TABLE desired_state (
    aggregate_type            TEXT NOT NULL,
    aggregate_id              BIGINT NOT NULL,
    version                   BIGINT NOT NULL DEFAULT 1,
    document                  JSONB NOT NULL,
    rendered_digest           TEXT NOT NULL,
    published_digest          TEXT NULL,
    published_artifact_digest TEXT NULL,
    published_at              TIMESTAMPTZ NULL,
    last_error                TEXT NULL,
    updated_at                TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (aggregate_type, aggregate_id)
);

-- +goose Down
DROP TABLE desired_state;
DROP TABLE group_artifacts;
DROP TABLE event_outbox;
