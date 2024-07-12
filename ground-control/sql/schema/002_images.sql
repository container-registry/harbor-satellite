-- +goose Up
CREATE TABLE images (
  group_id INT REFERENCES groups(id),
  image_list JSONB NOT NULL,
  PRIMARY KEY (group_id)
);


-- +goose Down
DROP TABLE images;
