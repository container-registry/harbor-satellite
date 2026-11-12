-- name: BatchInsertArtifacts :exec
INSERT INTO artifacts (reference, size_bytes)
SELECT unnest(@refs::TEXT[]), unnest(@sizes::BIGINT[])
ON CONFLICT (reference) DO NOTHING;

-- name: GetArtifactIDsByReferences :many
SELECT id, reference, size_bytes, created_at FROM artifacts
WHERE reference = ANY(@refs::TEXT[]);

-- name: DeleteOrphanedArtifacts :exec
-- The inventory of plan item 4.4 lives in satellite_view; older status
-- rows still reference artifacts through artifact_ids.
DELETE FROM artifacts
WHERE id NOT IN (
    SELECT unnest(artifact_ids) FROM satellite_status
    WHERE artifact_ids IS NOT NULL
    UNION
    SELECT unnest(inventory_artifact_ids) FROM satellite_view
    WHERE inventory_artifact_ids IS NOT NULL
)
AND created_at < NOW() - INTERVAL '1 day' * @retention_days;
