-- name: InsertSatelliteCachedImage :exec
INSERT INTO satellite_cached_images (satellite_id, reference, size_bytes, reported_at)
VALUES ($1, $2, $3, $4);

-- name: GetLatestCachedImages :many
SELECT sci.* FROM satellite_cached_images sci
WHERE sci.satellite_id = $1
  AND sci.reported_at = (
    SELECT MAX(reported_at) FROM satellite_cached_images WHERE satellite_id = $1
  )
ORDER BY sci.reference;

-- name: DeleteOldCachedImages :exec
DELETE FROM satellite_cached_images
WHERE created_at < NOW() - INTERVAL '1 day' * $1;
