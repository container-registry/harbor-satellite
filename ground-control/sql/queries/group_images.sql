-- name: AssignImageToGroup :exec
INSERT INTO group_images (group_id, image_id)
VALUES ($1, $2)
ON CONFLICT DO NOTHING;

-- name: RemoveImageFromGroup :exec
DELETE FROM group_images
WHERE group_id = $1 AND image_id = $2;

-- name: GetImagesForGroup :many
SELECT *
FROM images
JOIN group_images ON images.id = group_images.image_id
WHERE group_images.group_id = $1;
