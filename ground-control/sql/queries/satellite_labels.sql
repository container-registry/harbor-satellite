-- name: AddLabelToSatellite :exec
INSERT INTO satellite_labels (satellite_id, label_id)
VALUES ($1, $2)
ON CONFLICT DO NOTHING;
