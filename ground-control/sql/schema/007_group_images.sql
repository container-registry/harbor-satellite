-- +goose Up

CREATE TABLE group_images (
    group_id INT REFERENCES groups(id) ON DELETE CASCADE,
    image_id INT REFERENCES images(id) ON DELETE CASCADE,
    PRIMARY KEY (group_id, image_id)
);

-- +goose Down
DROP TABLE group_images;
