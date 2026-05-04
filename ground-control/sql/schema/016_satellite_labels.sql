-- +goose Up

CREATE TABLE satellite_labels (
    satellite_id INTEGER NOT NULL REFERENCES satellites(id) ON DELETE CASCADE,
    key         TEXT NOT NULL CHECK (length(key) BETWEEN 1 AND 316),
    value       TEXT NOT NULL CHECK (length(value) <= 63),
    PRIMARY KEY (satellite_id, key)
);

CREATE INDEX idx_satellite_labels_satellite_id ON satellite_labels(satellite_id);

-- +goose Down
DROP INDEX IF EXISTS idx_satellite_labels_satellite_id;
DROP TABLE satellite_labels;
