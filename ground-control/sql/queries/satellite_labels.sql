/* tsqllint-disable */

-- name: GetLabelsBySatelliteID :many
SELECT key, value FROM satellite_labels
WHERE satellite_id = $1
ORDER BY key;

-- name: SetLabels :exec
-- Deletes all labels for a satellite; use within a transaction with subsequent inserts.
DELETE FROM satellite_labels WHERE satellite_id = $1;

-- name: InsertLabel :exec
INSERT INTO satellite_labels (satellite_id, key, value)
VALUES ($1, $2, $3)
ON CONFLICT (satellite_id, key) DO UPDATE SET value = EXCLUDED.value;

-- name: DeleteLabel :exec
DELETE FROM satellite_labels WHERE satellite_id = $1 AND key = $2;
