-- name: InsertSatelliteStatus :one
INSERT INTO satellite_status (
    satellite_id, activity, latest_state_digest, latest_config_digest,
    cpu_percent, memory_used_bytes, storage_used_bytes,
    last_sync_duration_ms, image_count, reported_at, artifact_ids
)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)
RETURNING *;

-- name: UpdateSatelliteLastSeen :exec
UPDATE satellites SET last_seen = NOW(), heartbeat_interval = $2 WHERE id = $1;

-- name: GetStaleSatellites :many
SELECT s.id, s.name, s.created_at, s.updated_at, s.last_seen, s.heartbeat_interval,
       EXTRACT(EPOCH FROM (NOW() - s.last_seen))::BIGINT as seconds_since_seen
FROM satellites s
WHERE s.last_seen IS NOT NULL
  AND s.heartbeat_interval IS NOT NULL
  AND s.last_seen < NOW() - (
    CASE
      WHEN s.heartbeat_interval LIKE '@every %'
      THEN (SUBSTRING(s.heartbeat_interval FROM 8)::INTERVAL * 3)
      ELSE INTERVAL '15 minutes'
    END
  );

-- name: GetActiveSatellites :many
SELECT s.id, s.name, s.created_at, s.updated_at, s.last_seen, s.heartbeat_interval,
       COALESCE(ss.activity, '') as last_activity,
       COALESCE(ss.reported_at, s.last_seen) as last_status_time
FROM satellites s
LEFT JOIN LATERAL (
    SELECT activity, reported_at
    FROM satellite_status
    WHERE satellite_id = s.id
    ORDER BY created_at DESC LIMIT 1
) ss ON true
WHERE s.last_seen IS NOT NULL
  AND s.last_seen > NOW() - (
    CASE
      WHEN s.heartbeat_interval LIKE '@every %'
      THEN (SUBSTRING(s.heartbeat_interval FROM 8)::INTERVAL * 3)
      ELSE INTERVAL '15 minutes'
    END
  );

-- name: DeleteOldSatelliteStatus :exec
DELETE FROM satellite_status
WHERE created_at < NOW() - INTERVAL '1 day' * $1;

-- name: GetLatestSatelliteStatus :one
SELECT * FROM satellite_status
WHERE satellite_id = $1 ORDER BY created_at DESC LIMIT 1;

-- name: GetSatelliteStatusHistory :many
SELECT * FROM satellite_status
WHERE satellite_id = $1
ORDER BY created_at DESC
LIMIT $2;

-- name: GetLatestArtifacts :many
SELECT a.id, a.reference, a.size_bytes, a.created_at
FROM artifacts a
WHERE a.id = ANY(
    (SELECT artifact_ids FROM satellite_status
     WHERE satellite_id = $1
     ORDER BY created_at DESC LIMIT 1)
)
ORDER BY a.reference;
