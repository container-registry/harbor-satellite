-- Adapter-only queries: the satellites.sql queries stay unchanged because
-- handlers serialise their rows.

-- name: UpsertSatelliteIdentity :exec
INSERT INTO satellite_identities (satellite_id, spiffe_id, spire_entry_ids) VALUES ($1, $2, $3)
ON CONFLICT (satellite_id) DO UPDATE SET spiffe_id = EXCLUDED.spiffe_id, spire_entry_ids = EXCLUDED.spire_entry_ids;

-- name: GetFleetSatelliteByID :one
SELECT s.id, s.name, s.created_at, s.updated_at, s.last_seen, s.heartbeat_interval, COALESCE(i.spiffe_id, '')::TEXT AS spiffe_id
FROM satellites s LEFT JOIN satellite_identities i ON i.satellite_id = s.id
WHERE s.id = $1;

-- name: GetFleetSatelliteByName :one
SELECT s.id, s.name, s.created_at, s.updated_at, s.last_seen, s.heartbeat_interval, COALESCE(i.spiffe_id, '')::TEXT AS spiffe_id
FROM satellites s LEFT JOIN satellite_identities i ON i.satellite_id = s.id
WHERE s.name = $1;

-- name: ListFleetSatellites :many
SELECT s.id, s.name, s.created_at, s.updated_at, s.last_seen, s.heartbeat_interval, COALESCE(i.spiffe_id, '')::TEXT AS spiffe_id
FROM satellites s LEFT JOIN satellite_identities i ON i.satellite_id = s.id
ORDER BY s.id;
