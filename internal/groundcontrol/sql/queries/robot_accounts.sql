-- name: AddRobotAccount :one
INSERT INTO robot_accounts (robot_name, robot_secret_hash, robot_id, satellite_id, robot_expiry, created_at, updated_at)
VALUES ($1, $2, $3, $4, $5, NOW(), NOW())
  ON CONFLICT (robot_id)
  DO UPDATE SET
  robot_name = EXCLUDED.robot_name,
  robot_secret_hash = EXCLUDED.robot_secret_hash,
  robot_expiry = EXCLUDED.robot_expiry,
  updated_at = NOW()
RETURNING *;

-- name: GetRobotAccount :one
SELECT * FROM robot_accounts
WHERE id = $1;

-- name: GetRobotAccBySatelliteID :one
SELECT * FROM robot_accounts
WHERE satellite_id = $1;

-- name: GetRobotAccByRobotName :one
SELECT * FROM robot_accounts
WHERE robot_name = $1;

-- name: GetRobotAuthByRobotName :one
-- Robot Basic auth of a satellite in one statement: the secret hash, the
-- expiry and the satellite's name.
SELECT r.robot_secret_hash, r.robot_expiry, s.name AS satellite_name
FROM robot_accounts r JOIN satellites s ON s.id = r.satellite_id
WHERE r.robot_name = $1;

-- name: ListRobotAccounts :many
SELECT * FROM robot_accounts;

-- name: DeleteRobotAccount :exec
DELETE FROM robot_accounts
WHERE id = $1;

-- name: UpdateRobotAccount :execrows
UPDATE robot_accounts
SET robot_name = $2,
    robot_secret_hash = $3,
    robot_id = $4,
    robot_expiry = $5,
    updated_at = NOW()
WHERE id = $1;
