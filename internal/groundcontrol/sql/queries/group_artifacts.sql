-- name: UpsertGroupArtifacts :exec
INSERT INTO group_artifacts (group_id, artifacts)
VALUES ($1, $2)
ON CONFLICT (group_id) DO UPDATE
SET artifacts = EXCLUDED.artifacts, updated_at = NOW();

-- name: GetGroupArtifacts :one
SELECT artifacts FROM group_artifacts
WHERE group_id = $1;

-- name: ListGroupsTrackingTag :many
-- The groups whose artifacts list repository under tag, with their
-- artifacts: the groups a push of that tag can move.
SELECT sqlc.embed(g), ga.artifacts
FROM groups g
JOIN group_artifacts ga ON ga.group_id = g.id
WHERE ga.artifacts @> jsonb_build_array(jsonb_build_object('repository', @repository::TEXT, 'tag', jsonb_build_array(@tag::TEXT)))
ORDER BY g.id;

-- name: ListGroupsOfSatelliteWithArtifacts :many
-- A satellite's groups with their artifacts in one statement; a group
-- stored before group_artifacts existed has none.
SELECT sqlc.embed(g), COALESCE(ga.artifacts, '[]'::JSONB)::JSONB AS artifacts
FROM satellite_groups sg
JOIN groups g ON g.id = sg.group_id
LEFT JOIN group_artifacts ga ON ga.group_id = g.id
WHERE sg.satellite_id = $1
ORDER BY g.id;
