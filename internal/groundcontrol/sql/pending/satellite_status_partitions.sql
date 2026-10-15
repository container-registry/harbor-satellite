-- Plan item 4.5. Not applied: neither goose nor sqlc reads sql/pending.
-- satellite_status becomes range-partitioned by created_at, so retention
-- drops a day's partition instead of deleting rows. PartitionMaintainer
-- creates the daily partitions; until it has, rows land in the default one.

-- +goose Up
-- +goose StatementBegin
DROP INDEX idx_satellite_status_satellite_id;
DROP INDEX idx_satellite_status_created_at;
ALTER TABLE satellite_status RENAME TO satellite_status_legacy;
ALTER INDEX satellite_status_pkey RENAME TO satellite_status_legacy_pkey;

-- A partitioned table's primary key must include the partition key.
CREATE TABLE satellite_status (LIKE satellite_status_legacy INCLUDING DEFAULTS, PRIMARY KEY (id, created_at))
    PARTITION BY RANGE (created_at);
-- LIKE copies no foreign keys.
ALTER TABLE satellite_status ADD CONSTRAINT satellite_status_satellite_id_fkey
    FOREIGN KEY (satellite_id) REFERENCES satellites(id) ON DELETE CASCADE;
-- Keeps the id sequence when the legacy table is dropped.
ALTER SEQUENCE satellite_status_id_seq OWNED BY satellite_status.id;

CREATE TABLE satellite_status_default PARTITION OF satellite_status DEFAULT;
CREATE INDEX idx_satellite_status_satellite_created ON satellite_status (satellite_id, created_at DESC);

INSERT INTO satellite_status SELECT * FROM satellite_status_legacy;
DROP TABLE satellite_status_legacy;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
CREATE TABLE satellite_status_legacy (LIKE satellite_status INCLUDING DEFAULTS, PRIMARY KEY (id));
ALTER SEQUENCE satellite_status_id_seq OWNED BY satellite_status_legacy.id;
INSERT INTO satellite_status_legacy SELECT * FROM satellite_status;
DROP TABLE satellite_status;

ALTER TABLE satellite_status_legacy RENAME TO satellite_status;
ALTER INDEX satellite_status_legacy_pkey RENAME TO satellite_status_pkey;
ALTER TABLE satellite_status ADD CONSTRAINT satellite_status_satellite_id_fkey
    FOREIGN KEY (satellite_id) REFERENCES satellites(id) ON DELETE CASCADE;
CREATE INDEX idx_satellite_status_satellite_id ON satellite_status (satellite_id);
CREATE INDEX idx_satellite_status_created_at ON satellite_status (created_at);
-- +goose StatementEnd
