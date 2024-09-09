-- +goose Up
CREATE TABLE satellite_images (
  satellite_id INT REFERENCES satellites(id) ON DELETE CASCADE,
  image_id INT REFERENCES images(id) ON DELETE CASCADE,
  PRIMARY KEY (satellite_id, image_id)
);

-- +goose Down
DROP TABLE satellite_images;
