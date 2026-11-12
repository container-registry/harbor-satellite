-- name: UpsertOIDCUser :one
-- An existing (issuer, subject) identity returns its user and refreshes its
-- groups and its role, which every login re-derives from OIDC_ROLE_MAP;
-- otherwise the user is created with an unusable password hash and linked.
-- No row means a local account holds the username, or a concurrent first
-- login of the same identity committed after this statement's snapshot
-- (READ COMMITTED); the adapter retries once.
WITH existing AS (
    UPDATE user_identities
    SET groups = @groups::TEXT[], email = sqlc.narg(email), last_login_at = @now::TIMESTAMPTZ
    WHERE issuer = @issuer::TEXT AND subject = @subject::TEXT
    RETURNING user_id
), role_updated AS (
    UPDATE users u SET role = @role::TEXT, updated_at = NOW()
    FROM existing e WHERE u.id = e.user_id
    RETURNING u.id
), created AS (
    INSERT INTO users (username, password_hash, role)
    SELECT @username::TEXT, '!oidc', @role::TEXT
    WHERE NOT EXISTS (SELECT 1 FROM existing)
    ON CONFLICT (username) DO NOTHING
    RETURNING id
), linked AS (
    INSERT INTO user_identities (user_id, issuer, subject, email, groups, last_login_at)
    SELECT id, @issuer::TEXT, @subject::TEXT, sqlc.narg(email), @groups::TEXT[], @now::TIMESTAMPTZ
    FROM created
    RETURNING user_id
)
SELECT user_id FROM existing
UNION ALL
SELECT user_id FROM linked;

-- name: InsertOIDCLoginState :exec
INSERT INTO oidc_login_states (state, code_verifier, nonce, binding_hash, expires_at)
VALUES (@state, @code_verifier, @nonce, @binding_hash, @expires_at);

-- name: TakeOIDCLoginState :one
-- Single use: a matching take deletes the state, whether or not the
-- callback then succeeds. A state never taken, or asked for with another
-- binding or after expiry, stays until DeleteExpiredOIDCLoginStates.
DELETE FROM oidc_login_states
WHERE state = @state AND binding_hash = @binding_hash AND expires_at > @now::TIMESTAMPTZ
RETURNING code_verifier, nonce;

-- name: DeleteExpiredOIDCLoginStates :execrows
DELETE FROM oidc_login_states WHERE expires_at <= @now::TIMESTAMPTZ;

-- name: GetSessionPrincipalByToken :one
-- GetSessionByToken plus the groups of every OIDC identity of the user;
-- now is a parameter.
SELECT s.*, u.username, u.role,
       COALESCE((SELECT array_agg(DISTINCT grp ORDER BY grp)
                 FROM user_identities ui, unnest(ui.groups) AS grp
                 WHERE ui.user_id = u.id), '{}')::TEXT[] AS groups
FROM sessions s
JOIN users u ON s.user_id = u.id
WHERE s.token = @token AND s.expires_at > @now::TIMESTAMPTZ;
