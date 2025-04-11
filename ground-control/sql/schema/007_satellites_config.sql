-- +goose Up

CREATE TABLE satellite_configs (
  satellite_id INT PRIMARY KEY REFERENCES satellites(id) ON DELETE CASCADE,
  config_id INT NOT NULL REFERENCES configs(id) ON DELETE CASCADE
);
-- +goose Down

DROP TABLE satellite_configs;
