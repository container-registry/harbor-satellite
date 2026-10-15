-- name: IssueSatelliteSession :exec
-- ZTR and re-ZTR: replaces every session of the satellite with the new one.
WITH gone AS (DELETE FROM satellite_sessions WHERE satellite_id = sqlc.arg(satellite_id))
INSERT INTO satellite_sessions (satellite_id, token_hash, expires_at)
VALUES (sqlc.arg(satellite_id), sqlc.arg(token_hash), sqlc.arg(expires_at)::TIMESTAMPTZ);

-- name: LockSatelliteSessions :exec
-- Holds the satellite's session lock (namespace
-- advisorylock.SatelliteSessions) until the transaction ends.
SELECT pg_advisory_xact_lock(sqlc.arg(namespace)::INT, sqlc.arg(satellite_id)::INT);

-- name: IssueFirstSatelliteSession :execrows
-- Migration path: issues a session only when the satellite has none. Run
-- it after LockSatelliteSessions in the same transaction: under READ
-- COMMITTED two concurrent NOT EXISTS checks both pass, while behind the
-- lock the second statement starts after the first commits.
INSERT INTO satellite_sessions (satellite_id, token_hash, expires_at)
SELECT sqlc.arg(satellite_id), sqlc.arg(token_hash), sqlc.arg(expires_at)::TIMESTAMPTZ
WHERE NOT EXISTS (SELECT 1 FROM satellite_sessions
                  WHERE satellite_id = sqlc.arg(satellite_id) AND expires_at > sqlc.arg(now)::TIMESTAMPTZ);

-- name: RenewSatelliteSession :exec
-- One successor per session: a second renewal of the same session replaces it.
INSERT INTO satellite_sessions (satellite_id, token_hash, expires_at, renewed_from)
VALUES (sqlc.arg(satellite_id), sqlc.arg(token_hash), sqlc.arg(expires_at)::TIMESTAMPTZ, sqlc.arg(renewed_from))
ON CONFLICT (renewed_from) DO UPDATE SET token_hash = EXCLUDED.token_hash, expires_at = EXCLUDED.expires_at;

-- name: GetSatelliteAuthByTokenHash :one
SELECT ss.id AS session_id, ss.expires_at, s.id AS satellite_id, s.name AS satellite_name,
       COALESCE(v.state, 'pending')::TEXT AS state,
       COALESCE(v.inventory_digest, '')::TEXT AS inventory_digest,
       (SELECT r.robot_expiry FROM robot_accounts r WHERE r.satellite_id = s.id ORDER BY r.updated_at DESC LIMIT 1) AS robot_expiry
FROM satellite_sessions ss
JOIN satellites s ON s.id = ss.satellite_id
LEFT JOIN satellite_view v ON v.satellite_id = s.id
WHERE ss.token_hash = $1;

-- name: GetSatelliteAuthByName :one
-- SVID and robot Basic satellites; has_session drives the migration path.
SELECT s.id AS satellite_id, s.name AS satellite_name,
       COALESCE(v.state, 'pending')::TEXT AS state,
       COALESCE(v.inventory_digest, '')::TEXT AS inventory_digest,
       (SELECT r.robot_expiry FROM robot_accounts r WHERE r.satellite_id = s.id ORDER BY r.updated_at DESC LIMIT 1) AS robot_expiry,
       EXISTS (SELECT 1 FROM satellite_sessions ss WHERE ss.satellite_id = s.id
               AND ss.expires_at > sqlc.arg(now)::TIMESTAMPTZ) AS has_session
FROM satellites s LEFT JOIN satellite_view v ON v.satellite_id = s.id
WHERE s.name = sqlc.arg(name);

-- name: DeleteSatelliteSessions :exec
DELETE FROM satellite_sessions WHERE satellite_id = $1;

-- name: DeleteExpiredSatelliteSessions :execrows
DELETE FROM satellite_sessions WHERE expires_at < sqlc.arg(now)::TIMESTAMPTZ;
