-- +goose Up
-- OIDC identities of operators (plan item 8.1, ADR-0018). A JIT user is
-- created only with a role mapping and never adopts a local account.
CREATE TABLE user_identities (
    id            BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    user_id       INT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    issuer        TEXT NOT NULL,
    subject       TEXT NOT NULL,
    email         TEXT NULL,
    groups        TEXT[] NOT NULL DEFAULT '{}',
    last_login_at TIMESTAMPTZ NULL,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (issuer, subject)
);
CREATE INDEX idx_user_identities_user_id ON user_identities (user_id);

-- binding_hash is the SHA-256 of the cookie that ties the login to the browser.
CREATE TABLE oidc_login_states (
    state         TEXT PRIMARY KEY,
    code_verifier TEXT NOT NULL,
    nonce         TEXT NOT NULL,
    binding_hash  BYTEA NOT NULL CHECK (octet_length(binding_hash) = 32),
    expires_at    TIMESTAMPTZ NOT NULL
);

-- Robot rotation (plan item 8.4). One row per satellite keeps the last
-- generation; robot_accounts keeps UNIQUE(satellite_id) while the two
-- robots overlap. The successor's secret is sealed until it is promoted.
CREATE TABLE robot_rotations (
    satellite_id      INT PRIMARY KEY REFERENCES satellites(id) ON DELETE CASCADE,
    generation        BIGINT NOT NULL CHECK (generation > 0),
    robot_name        VARCHAR(255) NOT NULL UNIQUE,
    robot_harbor_id   BIGINT NOT NULL,
    secret_ciphertext BYTEA NOT NULL,
    secret_hash       TEXT NOT NULL,
    expires_at        TIMESTAMPTZ NULL,
    created_at        TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    acked_at          TIMESTAMPTZ NULL,
    promoted_at       TIMESTAMPTZ NULL
);

-- +goose Down
DROP TABLE robot_rotations;
DROP TABLE oidc_login_states;
DROP TABLE user_identities;
