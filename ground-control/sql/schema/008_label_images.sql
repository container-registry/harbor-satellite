-- +goose Up

CREATE TABLE label_images (
  label_id INT REFERENCES labels(id) ON DELETE CASCADE,
  image_id INT REFERENCES images(id) ON DELETE CASCADE,
  PRIMARY KEY (label_id, image_id)
);

-- +goose Down
DROP TABLE label_images;
