-- name: GetImage :one
SELECT * FROM images
WHERE id = $1;

-- name: ListImages :many
SELECT * FROM images;

-- name: AddImage :one
INSERT INTO images (registry, repository, tag, digest, created_at, updated_at)
VALUES ($1, $2, $3, $4, $5, $6)
RETURNING *;

-- name: DeleteImage :exec
DELETE FROM images
WHERE id = $1;

-- name: GetReposOfSatellite :many
SELECT i.repository
FROM satellite_groups sg
JOIN group_images gi
  ON sg.group_id = gi.group_id
JOIN images i
  ON gi.image_id = i.id
WHERE sg.satellite_id = $1

UNION

SELECT i.repository
FROM satellite_labels sl
JOIN label_images li
  ON sl.label_id = li.label_id
JOIN images i
  ON li.image_id = i.id
WHERE sl.satellite_id = $1;

-- -- name: GetImagesForSatellite :many
-- WITH satellite_groups AS (
--     SELECT group_id FROM satellite_groups WHERE satellite_id = (SELECT id FROM satellites WHERE satellites.token = $1)
-- ),
-- satellite_labels AS (
--     SELECT label_id FROM satellite_labels WHERE satellite_id = (SELECT id FROM satellites WHERE satellites.token = $1)
-- ),
-- all_images AS (
--     SELECT image_id FROM group_images WHERE group_id IN (SELECT group_id FROM satellite_groups)
--     UNION
--     SELECT image_id FROM label_images WHERE label_id IN (SELECT label_id FROM satellite_labels)
-- )
-- SELECT *
-- FROM images
-- WHERE id IN (SELECT image_id FROM all_images);
