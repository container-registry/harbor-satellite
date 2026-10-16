-- name: ListRobotsExpiringBefore :many
-- Robots expiring before @before without an unpromoted rotation, with the
-- generation their successor gets. Only satellites with a session
-- unexpired at @now or a SPIFFE identity are listed: a robot-Basic beat
-- cannot deliver the successor, so its rotation would never be acked.
-- robot_expiry holds UTC wall time, as PromoteRobotRotation writes it, so
-- it is read AT TIME ZONE 'UTC' against the instant @before.
SELECT ra.*,
       (COALESCE((SELECT rr.generation FROM robot_rotations rr WHERE rr.satellite_id = ra.satellite_id), 0) + 1)::BIGINT AS next_generation
FROM robot_accounts ra
WHERE (ra.robot_expiry AT TIME ZONE 'UTC') < @before::TIMESTAMPTZ
  AND NOT EXISTS (SELECT 1 FROM robot_rotations rr WHERE rr.satellite_id = ra.satellite_id AND rr.promoted_at IS NULL)
  AND (EXISTS (SELECT 1 FROM satellite_sessions ss WHERE ss.satellite_id = ra.satellite_id AND ss.expires_at > @now::TIMESTAMPTZ)
       OR EXISTS (SELECT 1 FROM satellite_identities si WHERE si.satellite_id = ra.satellite_id))
ORDER BY ra.robot_expiry
LIMIT @row_limit;

-- name: StageRobotRotation :one
-- Replaces the row only when the previous rotation is promoted and the
-- generation follows it; no row means another rotation is in flight. A
-- satellite's first rotation is generation 1.
INSERT INTO robot_rotations (satellite_id, generation, robot_name, robot_harbor_id, secret_ciphertext, secret_hash, expires_at)
SELECT @satellite_id::INT, @generation::BIGINT, @robot_name::TEXT, @robot_harbor_id::BIGINT,
       @secret_ciphertext::BYTEA, @secret_hash::TEXT, sqlc.narg(expires_at)::TIMESTAMPTZ
WHERE @generation::BIGINT = 1 OR EXISTS (SELECT 1 FROM robot_rotations rr WHERE rr.satellite_id = @satellite_id::INT)
ON CONFLICT (satellite_id) DO UPDATE SET
    generation        = EXCLUDED.generation,
    robot_name        = EXCLUDED.robot_name,
    robot_harbor_id   = EXCLUDED.robot_harbor_id,
    secret_ciphertext = EXCLUDED.secret_ciphertext,
    secret_hash       = EXCLUDED.secret_hash,
    expires_at        = EXCLUDED.expires_at,
    created_at        = NOW(),
    acked_at          = NULL,
    promoted_at       = NULL
WHERE robot_rotations.promoted_at IS NOT NULL
  AND EXCLUDED.generation = robot_rotations.generation + 1
RETURNING generation;

-- name: ListPendingRobotRotations :many
SELECT satellite_id, generation, robot_name, secret_ciphertext, expires_at
FROM robot_rotations
WHERE acked_at IS NULL AND promoted_at IS NULL
ORDER BY satellite_id;

-- name: AckRobotRotations :exec
-- @acks is a JSON array of {satellite_id, generation}.
UPDATE robot_rotations rr
SET acked_at = @now::TIMESTAMPTZ
FROM jsonb_to_recordset(@acks::JSONB) AS a(satellite_id INT, generation BIGINT)
WHERE rr.satellite_id = a.satellite_id
  AND rr.generation = a.generation
  AND rr.acked_at IS NULL
  AND rr.promoted_at IS NULL;

-- name: ListAckedRobotRotations :many
SELECT rr.satellite_id, rr.generation, rr.robot_name, rr.robot_harbor_id, rr.expires_at,
       ra.id AS previous_id, ra.robot_name AS previous_name, ra.robot_id::BIGINT AS previous_harbor_id
FROM robot_rotations rr
JOIN robot_accounts ra ON ra.satellite_id = rr.satellite_id
WHERE rr.acked_at IS NOT NULL AND rr.promoted_at IS NULL
ORDER BY rr.acked_at
LIMIT @row_limit;

-- name: PromoteRobotRotation :execrows
-- Moves the successor into robot_accounts and drops its sealed secret, only
-- while robot_accounts still holds the previous robot, so a rotation is
-- never marked promoted without the move. 0 rows then and when already
-- promoted.
WITH promoted AS (
    UPDATE robot_rotations
    SET promoted_at = @now::TIMESTAMPTZ, secret_ciphertext = '\x'::BYTEA
    WHERE robot_rotations.satellite_id = @satellite_id
      AND robot_rotations.generation = @generation
      AND acked_at IS NOT NULL
      AND promoted_at IS NULL
      AND EXISTS (SELECT 1 FROM robot_accounts prev
                  WHERE prev.satellite_id = @satellite_id AND prev.robot_id::BIGINT = @previous_harbor_id::BIGINT)
    RETURNING robot_rotations.satellite_id, robot_rotations.robot_name, robot_rotations.robot_harbor_id,
              robot_rotations.secret_hash, robot_rotations.expires_at
)
UPDATE robot_accounts ra
SET robot_name        = p.robot_name,
    robot_id          = p.robot_harbor_id::TEXT,
    robot_secret_hash = p.secret_hash,
    robot_expiry      = p.expires_at AT TIME ZONE 'UTC',
    updated_at        = NOW()
FROM promoted p
WHERE ra.satellite_id = p.satellite_id;

-- name: ListRobotHarborIDsOfSatellite :many
-- The robot plus any unpromoted successor, so a tombstone deletes both.
SELECT robot_id::BIGINT AS harbor_id FROM robot_accounts WHERE robot_accounts.satellite_id = @satellite_id
UNION
SELECT robot_harbor_id FROM robot_rotations WHERE robot_rotations.satellite_id = @satellite_id AND promoted_at IS NULL
ORDER BY 1;
