-- name: RecordDesiredState :one
-- Always returns the row: version moves only when the digest changes, and
-- publish stays true until the rendered digest is the published one.
INSERT INTO desired_state (aggregate_type, aggregate_id, document, rendered_digest)
VALUES (sqlc.arg(aggregate_type), sqlc.arg(aggregate_id), sqlc.arg(document), sqlc.arg(rendered_digest))
ON CONFLICT (aggregate_type, aggregate_id) DO UPDATE
SET version = desired_state.version + CASE WHEN desired_state.rendered_digest = EXCLUDED.rendered_digest THEN 0 ELSE 1 END,
    document = EXCLUDED.document,
    rendered_digest = EXCLUDED.rendered_digest,
    updated_at = NOW()
RETURNING version, (published_digest IS DISTINCT FROM rendered_digest)::BOOLEAN AS publish;

-- name: MarkDesiredStatePublished :execrows
-- Matches only while digest is still the rendered one, so a slow publisher
-- cannot mark a newer rendering as published.
UPDATE desired_state
SET published_digest = sqlc.arg(digest)::TEXT,
    published_artifact_digest = sqlc.arg(artifact_digest)::TEXT,
    published_at = NOW(),
    last_error = NULL
WHERE aggregate_type = sqlc.arg(aggregate_type)
  AND aggregate_id = sqlc.arg(aggregate_id)
  AND rendered_digest = sqlc.arg(digest)::TEXT;

-- name: ClearDesiredStatePublished :exec
-- Forces the next reconcile to publish again, after a tombstone of an
-- older aggregate with the same name deleted what was published.
UPDATE desired_state
SET published_digest = NULL, published_artifact_digest = NULL, published_at = NULL
WHERE aggregate_type = $1 AND aggregate_id = $2;

-- name: MarkDesiredStateFailed :exec
UPDATE desired_state
SET last_error = sqlc.arg(last_error)::TEXT
WHERE aggregate_type = sqlc.arg(aggregate_type) AND aggregate_id = sqlc.arg(aggregate_id);

-- name: GetDesiredState :one
SELECT * FROM desired_state
WHERE aggregate_type = $1 AND aggregate_id = $2;

-- name: DeleteDesiredState :exec
DELETE FROM desired_state
WHERE aggregate_type = $1 AND aggregate_id = $2;
