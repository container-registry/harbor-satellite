-- name: GetImage :one
SELECT * FROM images
WHERE id = $1;

-- name: AddImage :one
INSERT INTO images (registry, repository, tag, digest, created_at, updated_at)
VALUES ($1, $2, $3, $4, $5, $6)
RETURNING *;

-- name: DeleteImageList :exec
DELETE FROM images
WHERE id = $1;

-- name: GetImagesForSatellite :many
WITH satellite_groups AS (
    SELECT group_id FROM satellite_groups WHERE satellite_id = (SELECT id FROM satellites WHERE satellites.token = $1)
),
satellite_labels AS (
    SELECT label_id FROM satellite_labels WHERE satellite_id = (SELECT id FROM satellites WHERE satellites.token = $1)
),
group_images AS (
    SELECT image_id FROM group_images WHERE group_id IN (SELECT group_id FROM satellite_groups)
),
label_images AS (
    SELECT image_id FROM label_images WHERE label_id IN (SELECT label_id FROM satellite_labels)
),
all_images AS (
    SELECT image_id FROM group_images
    UNION
    SELECT image_id FROM label_images
)
SELECT *
FROM images
WHERE id IN (SELECT image_id FROM all_images);
