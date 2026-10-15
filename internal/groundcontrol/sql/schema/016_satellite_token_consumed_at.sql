-- +goose Up

ALTER TABLE satellite_token ADD COLUMN consumed_at TIMESTAMPTZ NULL;

-- +goose Down
ALTER TABLE satellite_token DROP COLUMN consumed_at;
