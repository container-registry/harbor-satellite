-- +goose Up
-- Ordered reads by (xid, id) for webhook checkpoints (ADR-0019).
CREATE INDEX idx_event_outbox_xid_id ON event_outbox (xid, id);
-- id is INT so it fits the two-key int4 advisory lock.
CREATE TABLE webhook_subscriptions (
    id                INT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    url               TEXT NOT NULL,
    secret_ciphertext BYTEA NOT NULL,
    event_types       TEXT[] NOT NULL DEFAULT '{}',
    enabled           BOOLEAN NOT NULL DEFAULT TRUE,
    disabled_reason   TEXT NULL,
    created_at        TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    created_by        TEXT NULL
);
CREATE TABLE webhook_checkpoints (
    subscription_id INT PRIMARY KEY REFERENCES webhook_subscriptions(id) ON DELETE CASCADE,
    last_xid        XID8 NOT NULL,
    last_id         BIGINT NOT NULL DEFAULT 0,
    attempts        INT NOT NULL DEFAULT 0,
    failing_since   TIMESTAMPTZ NULL,
    next_attempt_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    last_status     INT NULL,
    last_error      TEXT NULL,
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- +goose Down
DROP TABLE webhook_checkpoints;
DROP TABLE webhook_subscriptions;
DROP INDEX idx_event_outbox_xid_id;
