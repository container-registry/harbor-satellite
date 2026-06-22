-- ground-control/sql/queries/vulnerabilities.sql

-- name: UpsertVulnerability :exec
INSERT INTO vulnerabilities (
    artifact_digest,
    cve_id,
    severity,
    package_name,
    installed_version,
    fixed_version,
    description,
    scanner,
    scanned_at
) VALUES (
    $1, $2, $3, $4, $5, $6, $7, $8, $9
)
ON CONFLICT (artifact_digest, cve_id) DO UPDATE SET
    severity = EXCLUDED.severity,
    package_name = EXCLUDED.package_name,
    installed_version = EXCLUDED.installed_version,
    fixed_version = EXCLUDED.fixed_version,
    description = EXCLUDED.description,
    scanner = EXCLUDED.scanner,
    scanned_at = EXCLUDED.scanned_at;

-- name: GetVulnerabilitiesByArtifact :many
SELECT * FROM vulnerabilities
WHERE artifact_digest = $1
ORDER BY severity DESC;

-- name: DeleteVulnerabilitiesForArtifact :exec
DELETE FROM vulnerabilities
WHERE artifact_digest = $1;