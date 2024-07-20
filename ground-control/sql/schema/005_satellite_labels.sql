-- +goose Up

CREATE TABLE satellite_labels (
  satellite_id INT REFERENCES satellites(id) ON DELETE CASCADE,
  label_id INT REFERENCES labels(id) ON DELETE CASCADE,
  PRIMARY KEY (satellite_id, label_id)
);

-- +goose Down
DROP TABLE satellite_labels;
