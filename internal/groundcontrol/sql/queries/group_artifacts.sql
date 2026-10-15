-- name: UpsertGroupArtifacts :exec
INSERT INTO group_artifacts (group_id, artifacts)
VALUES ($1, $2)
ON CONFLICT (group_id) DO UPDATE
SET artifacts = EXCLUDED.artifacts, updated_at = NOW();

-- name: GetGroupArtifacts :one
SELECT artifacts FROM group_artifacts
WHERE group_id = $1;
