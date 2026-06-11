CREATE TABLE vulnerabilities (
    id SERIAL PRIMARY KEY,
    artifact_digest VARCHAR(255) NOT NULL,
    cve_id VARCHAR(50) NOT NULL,
    severity VARCHAR(20) NOT NULL,
    package_name VARCHAR(255),
    installed_version VARCHAR(100),
    fixed_version VARCHAR(100),
    description TEXT,
    scanner VARCHAR(50),
    scanned_at TIMESTAMP,
    created_at TIMESTAMP DEFAULT NOW(),
    UNIQUE(artifact_digest, cve_id)
);

CREATE INDEX idx_vulnerabilities_artifact_digest ON vulnerabilities (artifact_digest);
CREATE INDEX idx_vulnerabilities_severity ON vulnerabilities (severity);