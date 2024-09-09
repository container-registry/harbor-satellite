-- name: AssignImageToSatellite :exec
INSERT INTO satellite_images (satellite_id, image_id)
VALUES ($1, $2)
  ON CONFLICT DO NOTHING;

-- name: RemoveImageFromSatellite :exec
DELETE FROM satellite_images
WHERE satellite_id = $1 AND image_id = $2;

-- name: GetImagesForSatellite :many
SELECT *
FROM images
JOIN satellite_images ON images.id = satellite_images.image_id
WHERE satellite_images.satellite_id = $1;
