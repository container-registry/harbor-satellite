-- name: BatchInsertArtifacts :exec
INSERT INTO artifacts (reference, size_bytes)
SELECT unnest(@refs::TEXT[]), unnest(@sizes::BIGINT[])
ON CONFLICT (reference) DO NOTHING;

-- name: GetArtifactIDsByReferences :many
SELECT id, reference, size_bytes, created_at FROM artifacts
WHERE reference = ANY(@refs::TEXT[]);

-- name: GetArtifactsByIDs :many
SELECT id, reference, size_bytes, created_at FROM artifacts
WHERE id = ANY(@ids::INT[]);

-- name: DeleteOrphanedArtifacts :exec
DELETE FROM artifacts
WHERE id NOT IN (
    SELECT unnest(artifact_ids) FROM satellite_status
    WHERE artifact_ids IS NOT NULL
)
AND created_at < NOW() - INTERVAL '1 day' * @retention_days;
