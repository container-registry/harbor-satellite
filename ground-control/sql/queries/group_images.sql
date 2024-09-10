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

-- name: GetImagesForGroupAndSubgroups :many
WITH RECURSIVE GroupHierarchy AS (
  SELECT g.id AS group_id
  FROM groups g
  WHERE g.id = $1

  UNION ALL

  SELECT g2.id AS group_id
  FROM groups g2
  JOIN GroupHierarchy gh ON g2.parent_group_id = gh.group_id
)
SELECT i.*
FROM images i
JOIN group_images gi ON i.id = gi.image_id
WHERE gi.group_id IN (SELECT gh.group_id FROM GroupHierarchy gh);
