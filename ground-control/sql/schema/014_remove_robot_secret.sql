-- +goose Up
ALTER TABLE robot_accounts DROP COLUMN robot_secret;
ALTER TABLE robot_accounts ADD COLUMN robot_expiry TIMESTAMPTZ;

-- +goose Down
ALTER TABLE robot_accounts ADD COLUMN robot_secret VARCHAR(255) NOT NULL DEFAULT '';
ALTER TABLE robot_accounts DROP COLUMN robot_expiry;
