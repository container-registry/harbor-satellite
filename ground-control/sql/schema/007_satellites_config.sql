-- +goose Up

CREATE TABLE satellite_configs (
  satellite_id INT REFERENCES satellites(id) ON DELETE CASCADE,
  config_id INT REFERENCES configs(id) ON DELETE CASCADE,
  PRIMARY KEY (satellite_id, config_id)
);

-- +goose Down

DROP TABLE satellite_configs;
