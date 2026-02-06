-- +goose Up

CREATE TABLE satellite_cached_images (
    id SERIAL PRIMARY KEY,
    satellite_id INT NOT NULL REFERENCES satellites(id) ON DELETE CASCADE,
    reference VARCHAR(512) NOT NULL,
    size_bytes BIGINT NOT NULL,
    reported_at TIMESTAMP NOT NULL,
    created_at TIMESTAMP NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_cached_images_satellite_id ON satellite_cached_images(satellite_id);
CREATE INDEX idx_cached_images_satellite_reported ON satellite_cached_images(satellite_id, reported_at DESC);

-- +goose Down
DROP INDEX IF EXISTS idx_cached_images_satellite_reported;
DROP INDEX IF EXISTS idx_cached_images_satellite_id;
DROP TABLE satellite_cached_images;
