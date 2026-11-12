-- +goose Up
-- Stored policy of plan item 7.3 (ADR-0017). Casbin loads these on top of
-- the embedded baseline; every write bumps policy_version, which each GC
-- replica polls to reload.
CREATE TABLE policy_rules (
    id            BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    subject       TEXT NOT NULL,
    scope         TEXT NOT NULL DEFAULT '*',
    resource_type TEXT NOT NULL DEFAULT '*',
    action        TEXT NOT NULL DEFAULT '*',
    effect        TEXT NOT NULL CHECK (effect IN ('allow', 'deny')),
    created_at    TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    created_by    TEXT NULL,
    UNIQUE (subject, scope, resource_type, action, effect)
);
-- Additional scoped roles only; built-in roles come from users.role per request.
CREATE TABLE policy_role_bindings (
    id         BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    principal  TEXT NOT NULL,
    role       TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    created_by TEXT NULL,
    UNIQUE (principal, role)
);
CREATE TABLE policy_version (
    singleton  BOOLEAN PRIMARY KEY DEFAULT TRUE CHECK (singleton),
    version    BIGINT NOT NULL DEFAULT 0,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
INSERT INTO policy_version (singleton) VALUES (TRUE);

-- +goose Down
DROP TABLE policy_version;
DROP TABLE policy_role_bindings;
DROP TABLE policy_rules;
