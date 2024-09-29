-- name: AddRobotAccount :one
INSERT INTO robot_accounts (robot_name, robot_secret, satellite_id, created_at, updated_at)
VALUES ($1, $2, $3, NOW(), NOW())
RETURNING *;

-- name: GetRobotAccount :one
SELECT * FROM robot_accounts
WHERE id = $1;

-- name: ListRobotAccounts :many
SELECT * FROM robot_accounts;

-- name: DeleteRobotAccount :exec
DELETE FROM robot_accounts
WHERE id = $1;

-- name: UpdateRobotAccount :exec
UPDATE robot_accounts
SET robot_name = $2,
    robot_secret = $3,
    updated_at = NOW()
WHERE id = $1;
