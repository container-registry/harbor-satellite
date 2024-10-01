-- name: CreateLabel :one
INSERT INTO labels (label_name, created_at, updated_at)
VALUES ($1, $2, $3)
RETURNING *;

-- name: GetLabelByID :one
SELECT * FROM labels
WHERE id = $1;

-- name: GetLabelByName :one
SELECT * FROM labels
WHERE label_name = $1;

-- name: DeleteLabel :exec
DELETE FROM labels
WHERE id = $1;
