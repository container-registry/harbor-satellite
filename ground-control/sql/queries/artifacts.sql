-- name: BatchInsertArtifacts :exec
INSERT INTO artifacts (reference, size_bytes)
SELECT unnest(@refs::TEXT[]), unnest(@sizes::BIGINT[])
ON CONFLICT (reference) DO NOTHING;

-- name: GetArtifactIDsByReferences :many
SELECT id, reference, size_bytes, created_at FROM artifacts
WHERE reference = ANY(@refs::TEXT[]);

-- name: DeleteOrphanedArtifacts :exec
DELETE FROM artifacts
WHERE id NOT IN (
    SELECT unnest(artifact_ids) FROM satellite_status
    WHERE artifact_ids IS NOT NULL
)
AND created_at < NOW() - INTERVAL '1 day' * @retention_days;

-- name: GetImageDistribution :many
WITH latest_status AS (
    SELECT DISTINCT ON (satellite_id) 
        satellite_id, artifact_ids
    FROM satellite_status
    ORDER BY satellite_id, reported_at DESC
)
SELECT 
    a.reference,
    a.size_bytes,
    COUNT(DISTINCT ls.satellite_id)::BIGINT AS satellite_count,
    ARRAY_AGG(DISTINCT sat.name) AS satellites
FROM artifacts a
JOIN latest_status ls ON a.id = ANY(ls.artifact_ids)
JOIN satellites sat ON ls.satellite_id = sat.id
GROUP BY a.id, a.reference, a.size_bytes
ORDER BY satellite_count DESC;
