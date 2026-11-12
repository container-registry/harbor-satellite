-- +goose Up
-- Satellite session tokens (ADR-0016). Only the SHA-256 of a token is stored.
-- renewed_from allows one successor per session, so a satellite that keeps
-- the old token gets the successor replaced, not one row per beat.
CREATE TABLE satellite_sessions (
    id           BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    satellite_id INT NOT NULL REFERENCES satellites(id) ON DELETE CASCADE,
    token_hash   BYTEA NOT NULL UNIQUE CHECK (octet_length(token_hash) = 32),
    renewed_from BIGINT NULL UNIQUE REFERENCES satellite_sessions(id) ON DELETE SET NULL,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    expires_at   TIMESTAMPTZ NOT NULL
);
CREATE INDEX idx_satellite_sessions_satellite_id ON satellite_sessions (satellite_id);
CREATE INDEX idx_satellite_sessions_expires_at ON satellite_sessions (expires_at);

-- SPIFFE identity of satellites registered through SPIRE; a table of its
-- own so the satellites JSON stays as the golden tests freeze it.
CREATE TABLE satellite_identities (
    satellite_id    INT PRIMARY KEY REFERENCES satellites(id) ON DELETE CASCADE,
    spiffe_id       TEXT UNIQUE NOT NULL,
    spire_entry_ids TEXT[] NOT NULL DEFAULT '{}'
);

-- Read model of plan item 4.3 (ADR-0006). A missing row means pending.
-- last_reported_at stays TIMESTAMP like satellite_status.reported_at, so the
-- active-satellites JSON keeps its encoding. applied_states are the group
-- states the satellite reports applied, [{ref, digest}].
CREATE TABLE satellite_view (
    satellite_id           INT PRIMARY KEY REFERENCES satellites(id) ON DELETE CASCADE,
    state                  TEXT NOT NULL DEFAULT 'pending' CHECK (state IN ('pending', 'online', 'stale', 'revoked')),
    last_seen              TIMESTAMPTZ NULL,
    heartbeat_interval     TEXT NULL,
    last_activity          TEXT NULL,
    last_reported_at       TIMESTAMP NULL,
    applied_state_digest   TEXT NULL,
    applied_config_digest  TEXT NULL,
    applied_states         JSONB NOT NULL DEFAULT '[]'::JSONB,
    inventory_digest       TEXT NULL,
    inventory_artifact_ids INT[] NULL,
    last_replication_error TEXT NULL,
    revoked_at             TIMESTAMPTZ NULL,
    updated_at             TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX idx_satellite_view_state ON satellite_view (state);

-- +goose Down
DROP TABLE satellite_view;
DROP TABLE satellite_identities;
DROP TABLE satellite_sessions;
