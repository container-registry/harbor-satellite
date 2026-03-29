-- +goose Up

ALTER TABLE satellites ADD COLUMN mode TEXT NOT NULL DEFAULT 'normal';

-- +goose Down
ALTER TABLE satellites DROP COLUMN IF EXISTS mode;
