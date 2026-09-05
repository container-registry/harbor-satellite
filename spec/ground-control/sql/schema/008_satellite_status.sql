-- +goose Up

CREATE TABLE satellite_status (
    id SERIAL PRIMARY KEY,
    satellite_id INT NOT NULL REFERENCES satellites(id) ON DELETE CASCADE,
    activity VARCHAR(255) NOT NULL,
    latest_state_digest VARCHAR(255),
    latest_config_digest VARCHAR(255),
    cpu_percent DECIMAL(5,2),
    memory_used_bytes BIGINT,
    storage_used_bytes BIGINT,
    last_sync_duration_ms BIGINT,
    image_count INT,
    reported_at TIMESTAMP NOT NULL,
    created_at TIMESTAMP NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_satellite_status_satellite_id ON satellite_status(satellite_id);
CREATE INDEX idx_satellite_status_created_at ON satellite_status(created_at);

-- +goose Down
DROP INDEX IF EXISTS idx_satellite_status_created_at;
DROP INDEX IF EXISTS idx_satellite_status_satellite_id;
DROP TABLE satellite_status;
