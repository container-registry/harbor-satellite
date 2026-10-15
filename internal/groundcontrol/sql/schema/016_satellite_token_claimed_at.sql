-- +goose Up
ALTER TABLE satellite_token ADD COLUMN claimed_at TIMESTAMP;

-- +goose Down
ALTER TABLE satellite_token DROP COLUMN claimed_at;
