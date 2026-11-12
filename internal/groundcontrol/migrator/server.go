package migrator

import (
	"context"
	"database/sql"
	"fmt"
)

// MinServerVersionNum is PostgreSQL 19.0 (ADR-0020). Betas and release
// candidates of 19 report the same number, so they pass.
const MinServerVersionNum = 190000

// CheckServer refuses a PostgreSQL server that ADR-0020 does not support.
func CheckServer(ctx context.Context, db *sql.DB) error {
	var version, walLevel string
	var versionNum int
	err := db.QueryRowContext(ctx,
		`SELECT current_setting('server_version'), current_setting('server_version_num')::int, current_setting('wal_level')`,
	).Scan(&version, &versionNum, &walLevel)
	if err != nil {
		return fmt.Errorf("read PostgreSQL server settings: %w", err)
	}
	return validateServer(version, versionNum, walLevel)
}

func validateServer(version string, versionNum int, walLevel string) error {
	if versionNum < MinServerVersionNum {
		return fmt.Errorf("PostgreSQL %s is not supported: Ground Control needs PostgreSQL 19 or later (ADR-0020); upgrade the server with pg_upgrade or dump and restore", version)
	}
	if walLevel == "minimal" {
		return fmt.Errorf("PostgreSQL wal_level=minimal is not supported: Ground Control needs replica or logical for its logical replication slot (ADR-0020)")
	}
	return nil
}
