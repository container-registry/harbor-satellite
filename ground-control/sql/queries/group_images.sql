-- name: AssignImageToGroup :exec
INSERT INTO group_images (group_id, image_id)
VALUES ($1, $2)
ON CONFLICT DO NOTHING;

-- name: GetImagesForGroup :many
SELECT *
FROM images
JOIN group_images ON images.id = group_images.image_id
WHERE group_images.group_id = $1;
