-- +goose Up
CREATE TABLE artifacts (
    id         SERIAL PRIMARY KEY,
    reference  VARCHAR(512) UNIQUE NOT NULL,
    size_bytes BIGINT NOT NULL,
    created_at TIMESTAMP NOT NULL DEFAULT NOW()
);

ALTER TABLE satellite_status ADD COLUMN artifact_ids INT[];

DROP TABLE IF EXISTS satellite_cached_images;

-- +goose Down
-- No satellite_cached_images here: migration 014 that created it is gone,
-- so recreating it left a table no Down drops and reverting 001 failed.
ALTER TABLE satellite_status DROP COLUMN IF EXISTS artifact_ids;
DROP TABLE IF EXISTS artifacts;
