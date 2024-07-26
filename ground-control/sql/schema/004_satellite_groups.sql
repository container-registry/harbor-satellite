-- +goose Up

CREATE TABLE satellite_groups (
  satellite_id INT REFERENCES satellites(id) ON DELETE CASCADE,
  group_id INT REFERENCES groups(id) ON DELETE CASCADE,
  PRIMARY KEY (satellite_id, group_id)
);

-- +goose Down
DROP TABLE satellite_groups;
