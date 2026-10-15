-- Read model of plan item 4.3 (ADR-0006). The batch statements take rows
-- as a JSON array with at most one row per satellite; a duplicate fails
-- FlushSatelliteView's ON CONFLICT. The join with satellites skips a
-- satellite deleted between its beat and the flush. In FlushLastSeen and
-- FlushSatelliteView a row older than the stored last_seen changes
-- nothing; StoreSatelliteInventories keeps the last flush, and a stale
-- one is healed by the next beat's inventory_requested.
-- Times are read as TIMESTAMPTZ, so the TIMESTAMP columns get the session
-- time zone's wall clock, as NOW() writes them elsewhere.

-- name: FlushLastSeen :exec
-- rows: [{id, seen_at, heartbeat_interval}]; UpdateSatelliteLastSeen's NULL rule.
UPDATE satellites AS s
SET last_seen = r.seen_at, heartbeat_interval = NULLIF(r.heartbeat_interval, '')
FROM jsonb_to_recordset(sqlc.arg(rows)::JSONB) AS r(id INT, seen_at TIMESTAMPTZ, heartbeat_interval TEXT)
WHERE s.id = r.id AND (s.last_seen IS NULL OR r.seen_at >= s.last_seen);

-- name: FlushSatelliteView :exec
-- pending and stale become online; revoked stays revoked.
INSERT INTO satellite_view (satellite_id, state, last_seen, heartbeat_interval, last_activity, last_reported_at,
    applied_state_digest, applied_config_digest, applied_states, inventory_digest, last_replication_error, updated_at)
SELECT r.id, 'online', r.seen_at, NULLIF(r.heartbeat_interval, ''), r.activity, r.reported_at,
    NULLIF(r.state_digest, ''), NULLIF(r.config_digest, ''), COALESCE(r.applied_states, '[]'::JSONB),
    NULLIF(r.inventory_digest, ''), NULLIF(r.last_replication_error, ''), sqlc.arg(now)::TIMESTAMPTZ
FROM jsonb_to_recordset(sqlc.arg(rows)::JSONB) AS r(id INT, seen_at TIMESTAMPTZ, heartbeat_interval TEXT, activity TEXT,
    reported_at TIMESTAMPTZ, state_digest TEXT, config_digest TEXT, applied_states JSONB, inventory_digest TEXT,
    last_replication_error TEXT)
JOIN satellites s ON s.id = r.id
ON CONFLICT (satellite_id) DO UPDATE SET
    state = CASE WHEN satellite_view.state = 'revoked' THEN 'revoked' ELSE 'online' END,
    last_seen = EXCLUDED.last_seen,
    heartbeat_interval = EXCLUDED.heartbeat_interval,
    last_activity = EXCLUDED.last_activity,
    last_reported_at = EXCLUDED.last_reported_at,
    applied_state_digest = COALESCE(EXCLUDED.applied_state_digest, satellite_view.applied_state_digest),
    applied_config_digest = COALESCE(EXCLUDED.applied_config_digest, satellite_view.applied_config_digest),
    applied_states = EXCLUDED.applied_states,
    inventory_digest = COALESCE(EXCLUDED.inventory_digest, satellite_view.inventory_digest),
    last_replication_error = EXCLUDED.last_replication_error,
    updated_at = EXCLUDED.updated_at
WHERE satellite_view.last_seen IS NULL OR EXCLUDED.last_seen >= satellite_view.last_seen;

-- name: InsertSatelliteStatusBatch :exec
-- Zero values arrive as JSON null, like InsertSatelliteStatus's toNull*.
-- artifact_ids is not written: the inventory lives in satellite_view (4.4).
INSERT INTO satellite_status (satellite_id, activity, latest_state_digest, latest_config_digest, cpu_percent,
    memory_used_bytes, storage_used_bytes, last_sync_duration_ms, image_count, reported_at)
SELECT r.satellite_id, r.activity, r.latest_state_digest, r.latest_config_digest, r.cpu_percent, r.memory_used_bytes,
    r.storage_used_bytes, r.last_sync_duration_ms, r.image_count, r.reported_at
FROM jsonb_to_recordset(sqlc.arg(rows)::JSONB) AS r(satellite_id INT, activity TEXT, latest_state_digest TEXT,
    latest_config_digest TEXT, cpu_percent NUMERIC(5,2), memory_used_bytes BIGINT, storage_used_bytes BIGINT,
    last_sync_duration_ms BIGINT, image_count INT, reported_at TIMESTAMPTZ)
JOIN satellites s ON s.id = r.satellite_id;

-- name: StoreSatelliteInventories :exec
-- rows: [{satellite_id, digest, items:[{reference,size_bytes}]}]; empty items clear the inventory.
-- DO UPDATE ... RETURNING also returns the id of a reference another replica inserted concurrently.
-- jsonb_array_elements rather than jsonb_to_recordset: sqlc cannot resolve
-- a column definition list inside a CTE.
WITH inv AS (
    SELECT (r.elem->>'satellite_id')::INT AS satellite_id, r.elem->>'digest' AS digest, r.elem->'items' AS items
    FROM jsonb_array_elements(sqlc.arg(rows)::JSONB) AS r(elem)
    JOIN satellites s ON s.id = (r.elem->>'satellite_id')::INT
), item AS (
    SELECT inv.satellite_id, i.item->>'reference' AS reference, (i.item->>'size_bytes')::BIGINT AS size_bytes
    FROM inv CROSS JOIN LATERAL jsonb_array_elements(inv.items) AS i(item)
), artifact AS (
    INSERT INTO artifacts (reference, size_bytes)
    SELECT DISTINCT ON (reference) reference, size_bytes FROM item ORDER BY reference
    ON CONFLICT (reference) DO UPDATE SET reference = EXCLUDED.reference
    RETURNING id, reference
)
INSERT INTO satellite_view (satellite_id, inventory_digest, inventory_artifact_ids, updated_at)
SELECT inv.satellite_id, inv.digest,
       COALESCE((SELECT array_agg(DISTINCT a.id ORDER BY a.id) FROM item JOIN artifact a USING (reference)
                 WHERE item.satellite_id = inv.satellite_id), '{}'),
       sqlc.arg(now)::TIMESTAMPTZ
FROM inv
ON CONFLICT (satellite_id) DO UPDATE SET inventory_digest = EXCLUDED.inventory_digest,
    inventory_artifact_ids = EXCLUDED.inventory_artifact_ids, updated_at = EXCLUDED.updated_at;

-- name: RevokeSatelliteView :one
-- Returns the state before; revoking twice keeps the first revoked_at.
-- prev reads the statement's snapshot, so it sees the row as it was before
-- this statement. FOR UPDATE would skip the row this statement updates and
-- return pending; the upsert's row lock serialises revokes instead, and two
-- concurrent revokes may both report the earlier state, which only costs
-- the level-triggered reconciler a second, idempotent run.
WITH prev AS (SELECT state FROM satellite_view WHERE satellite_id = sqlc.arg(satellite_id))
INSERT INTO satellite_view (satellite_id, state, revoked_at, updated_at)
VALUES (sqlc.arg(satellite_id), 'revoked', sqlc.arg(now)::TIMESTAMPTZ, sqlc.arg(now)::TIMESTAMPTZ)
ON CONFLICT (satellite_id) DO UPDATE SET state = 'revoked',
    revoked_at = COALESCE(satellite_view.revoked_at, EXCLUDED.revoked_at), updated_at = EXCLUDED.updated_at
RETURNING COALESCE((SELECT state FROM prev), 'pending')::TEXT AS previous_state;

-- name: UnrevokeSatelliteView :execrows
UPDATE satellite_view SET state = 'pending', revoked_at = NULL, updated_at = sqlc.arg(now)::TIMESTAMPTZ
WHERE satellite_id = sqlc.arg(satellite_id) AND state = 'revoked';

-- name: MarkStaleSatellites :many
-- Stale after 3x the reported interval, else STALE_THRESHOLD; never sooner
-- than 2x the flush interval, so a 1s heartbeat does not flap. now is a parameter.
-- An interval not in normalizeHeartbeatInterval's form counts as none, so
-- one bad value cannot abort the pass for the fleet.
UPDATE satellite_view v SET state = 'stale', updated_at = sqlc.arg(now)::TIMESTAMPTZ
FROM satellites s
WHERE s.id = v.satellite_id AND v.state = 'online' AND v.last_seen < sqlc.arg(now)::TIMESTAMPTZ - GREATEST(
    CASE WHEN v.heartbeat_interval ~ '^@every [0-9]+h[0-9]{2}m[0-9]{2}s$' THEN SUBSTRING(v.heartbeat_interval FROM 8)::INTERVAL * 3
         ELSE make_interval(secs => sqlc.arg(default_threshold_seconds)::DOUBLE PRECISION) END,
    make_interval(secs => sqlc.arg(min_threshold_seconds)::DOUBLE PRECISION))
RETURNING v.satellite_id, s.name, v.last_seen;

-- name: ListSatelliteInventory :many
SELECT a.id, a.reference, a.size_bytes, a.created_at
FROM satellite_view v JOIN artifacts a ON a.id = ANY(v.inventory_artifact_ids)
WHERE v.satellite_id = $1 ORDER BY a.reference;

-- name: GetSatelliteViewByID :one
SELECT s.id, s.name, s.created_at, s.updated_at, s.last_seen, s.heartbeat_interval,
       COALESCE(v.state, 'pending')::TEXT AS state, v.last_activity, v.last_reported_at,
       v.applied_state_digest, v.applied_config_digest, COALESCE(v.applied_states, '[]'::JSONB)::JSONB AS applied_states,
       v.inventory_digest, v.last_replication_error, v.revoked_at, v.updated_at AS view_updated_at,
       d.version AS desired_version, d.rendered_digest, d.published_digest, d.published_artifact_digest,
       d.last_error AS publish_error, d.published_at,
       COALESCE((SELECT jsonb_agg(jsonb_build_object('group', g.group_name, 'published_artifact_digest', gd.published_artifact_digest)
                                  ORDER BY g.group_name)
                 FROM satellite_groups sg JOIN groups g ON g.id = sg.group_id
                 LEFT JOIN desired_state gd ON gd.aggregate_type = 'group' AND gd.aggregate_id = g.id
                 WHERE sg.satellite_id = s.id), '[]'::JSONB)::JSONB AS published_groups
FROM satellites s
LEFT JOIN satellite_view v ON v.satellite_id = s.id
LEFT JOIN desired_state d ON d.aggregate_type = 'satellite' AND d.aggregate_id = s.id
WHERE s.id = $1;

-- name: GetSatelliteViewByName :one
SELECT s.id, s.name, s.created_at, s.updated_at, s.last_seen, s.heartbeat_interval,
       COALESCE(v.state, 'pending')::TEXT AS state, v.last_activity, v.last_reported_at,
       v.applied_state_digest, v.applied_config_digest, COALESCE(v.applied_states, '[]'::JSONB)::JSONB AS applied_states,
       v.inventory_digest, v.last_replication_error, v.revoked_at, v.updated_at AS view_updated_at,
       d.version AS desired_version, d.rendered_digest, d.published_digest, d.published_artifact_digest,
       d.last_error AS publish_error, d.published_at,
       COALESCE((SELECT jsonb_agg(jsonb_build_object('group', g.group_name, 'published_artifact_digest', gd.published_artifact_digest)
                                  ORDER BY g.group_name)
                 FROM satellite_groups sg JOIN groups g ON g.id = sg.group_id
                 LEFT JOIN desired_state gd ON gd.aggregate_type = 'group' AND gd.aggregate_id = g.id
                 WHERE sg.satellite_id = s.id), '[]'::JSONB)::JSONB AS published_groups
FROM satellites s
LEFT JOIN satellite_view v ON v.satellite_id = s.id
LEFT JOIN desired_state d ON d.aggregate_type = 'satellite' AND d.aggregate_id = s.id
WHERE s.name = $1;

-- name: ListSatelliteViewsByState :many
SELECT s.id, s.name, s.created_at, s.updated_at, s.last_seen, s.heartbeat_interval,
       COALESCE(v.state, 'pending')::TEXT AS state, v.last_activity, v.last_reported_at,
       v.applied_state_digest, v.applied_config_digest, COALESCE(v.applied_states, '[]'::JSONB)::JSONB AS applied_states,
       v.inventory_digest, v.last_replication_error, v.revoked_at, v.updated_at AS view_updated_at,
       d.version AS desired_version, d.rendered_digest, d.published_digest, d.published_artifact_digest,
       d.last_error AS publish_error, d.published_at,
       COALESCE((SELECT jsonb_agg(jsonb_build_object('group', g.group_name, 'published_artifact_digest', gd.published_artifact_digest)
                                  ORDER BY g.group_name)
                 FROM satellite_groups sg JOIN groups g ON g.id = sg.group_id
                 LEFT JOIN desired_state gd ON gd.aggregate_type = 'group' AND gd.aggregate_id = g.id
                 WHERE sg.satellite_id = s.id), '[]'::JSONB)::JSONB AS published_groups
FROM satellites s
LEFT JOIN satellite_view v ON v.satellite_id = s.id
LEFT JOIN desired_state d ON d.aggregate_type = 'satellite' AND d.aggregate_id = s.id
WHERE COALESCE(v.state, 'pending') = sqlc.arg(state)::TEXT
ORDER BY s.id;
