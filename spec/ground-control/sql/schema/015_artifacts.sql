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
CREATE TABLE satellite_cached_images (
    id           SERIAL PRIMARY KEY,
    satellite_id INT NOT NULL REFERENCES satellites(id) ON DELETE CASCADE,
    reference    VARCHAR(512) NOT NULL,
    size_bytes   BIGINT NOT NULL,
    reported_at  TIMESTAMP NOT NULL,
    created_at   TIMESTAMP NOT NULL DEFAULT NOW()
);
CREATE INDEX idx_cached_images_satellite_id ON satellite_cached_images(satellite_id);
CREATE INDEX idx_cached_images_satellite_reported ON satellite_cached_images(satellite_id, reported_at DESC);

ALTER TABLE satellite_status DROP COLUMN IF EXISTS artifact_ids;
DROP TABLE IF EXISTS artifacts;
