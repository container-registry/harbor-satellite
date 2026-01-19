-- +goose Up

ALTER TABLE satellites ADD COLUMN last_seen TIMESTAMP;
ALTER TABLE satellites ADD COLUMN heartbeat_interval VARCHAR(50);

-- +goose Down
ALTER TABLE satellites DROP COLUMN IF EXISTS heartbeat_interval;
ALTER TABLE satellites DROP COLUMN IF EXISTS last_seen;
