-- +goose Up
ALTER TABLE satellite_status
    ADD COLUMN download_bytes_per_second DOUBLE PRECISION,
    ADD COLUMN bytes_in_window BIGINT,
    ADD COLUMN bytes_total BIGINT;

-- +goose Down
ALTER TABLE satellite_status
    DROP COLUMN IF EXISTS bytes_total,
    DROP COLUMN IF EXISTS bytes_in_window,
    DROP COLUMN IF EXISTS download_bytes_per_second;
