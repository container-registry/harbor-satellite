-- name: AssignImageToLabel :exec
INSERT INTO label_images (label_id, image_id)
VALUES ($1, $2)
ON CONFLICT DO NOTHING;

-- name: RemoveImageFromLabel :exec
DELETE FROM label_images
WHERE label_id = $1 AND image_id = $2;

-- name: GetImagesForLabel :many
SELECT *
FROM images
JOIN label_images ON images.id = label_images.image_id
WHERE label_images.label_id = $1;
