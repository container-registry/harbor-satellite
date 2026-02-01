-- name: AddRobotAccount :one
INSERT INTO robot_accounts (robot_name, robot_id, satellite_id, robot_expiry, created_at, updated_at)
VALUES ($1, $2, $3, $4, NOW(), NOW())
  ON CONFLICT (robot_id)
  DO UPDATE SET
  robot_name = EXCLUDED.robot_name,
  robot_expiry = EXCLUDED.robot_expiry,
  updated_at = NOW()
RETURNING *;

-- name: GetRobotAccount :one
SELECT * FROM robot_accounts
WHERE id = $1;

-- name: GetRobotAccBySatelliteID :one
SELECT * FROM robot_accounts
WHERE satellite_id = $1;

-- name: ListRobotAccounts :many
SELECT * FROM robot_accounts;

-- name: DeleteRobotAccount :exec
DELETE FROM robot_accounts
WHERE id = $1;

-- name: UpdateRobotAccount :exec
UPDATE robot_accounts
SET robot_name = $2,
    robot_id = $3,
    robot_expiry = $4,
    updated_at = NOW()
WHERE id = $1;
