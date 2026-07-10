-- +goose Up

ALTER TABLE satellite_token ADD COLUMN expires_at TIMESTAMP NOT NULL DEFAULT (NOW() + INTERVAL '24 hours');

-- +goose Down
ALTER TABLE satellite_token DROP COLUMN expires_at;
