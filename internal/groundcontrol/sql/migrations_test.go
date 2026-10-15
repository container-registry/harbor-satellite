package migrations_test

import (
	"bytes"
	"context"
	"database/sql"
	"fmt"
	"io/fs"
	"os"
	"testing"
	"testing/fstest"

	"github.com/container-registry/harbor-satellite/internal/groundcontrol/dbtest"
	migrations "github.com/container-registry/harbor-satellite/internal/groundcontrol/sql"
	"github.com/container-registry/harbor-satellite/internal/shared/testutil/pending"
	"github.com/pressly/goose/v3"
	"github.com/stretchr/testify/require"
)

func embeddedSchema(t *testing.T) fs.FS {
	t.Helper()
	schema, err := fs.Sub(migrations.Schema, "schema")
	require.NoError(t, err)
	return schema
}

// provider never closes: goose's Close would close the dbtest pool.
func provider(t *testing.T, db *sql.DB, schema fs.FS) *goose.Provider {
	t.Helper()
	p, err := goose.NewProvider(goose.DialectPostgres, db, schema)
	require.NoError(t, err)
	return p
}

func publicTables(t *testing.T, db *sql.DB) []string {
	t.Helper()
	rows, err := db.QueryContext(context.Background(),
		"SELECT table_name FROM information_schema.tables WHERE table_schema = 'public' AND table_name <> 'goose_db_version' ORDER BY table_name")
	require.NoError(t, err)
	defer rows.Close()
	var tables []string
	for rows.Next() {
		var name string
		require.NoError(t, rows.Scan(&name))
		tables = append(tables, name)
	}
	require.NoError(t, rows.Err())
	return tables
}

func TestMigrations_UpDownUp(t *testing.T) {
	pending.Plan(t, "3.4", "015's Down recreates satellite_cached_images of the removed 014 and nothing drops it, so reverting 001 fails with 2BP01")
	db := dbtest.New(t)
	ctx := context.Background()
	p := provider(t, db.DB, embeddedSchema(t))
	latest, err := dbtest.LatestMigration()
	require.NoError(t, err)
	tables := publicTables(t, db.DB)
	require.NotEmpty(t, tables)

	_, err = p.DownTo(ctx, 0)
	require.NoError(t, err)
	require.Empty(t, publicTables(t, db.DB))

	_, err = p.Up(ctx)
	require.NoError(t, err)
	version, err := p.GetDBVersion(ctx)
	require.NoError(t, err)
	require.Equal(t, latest, version)
	require.Equal(t, tables, publicTables(t, db.DB))
}

func insertDuplicateRobots(t *testing.T, db *sql.DB) {
	t.Helper()
	ctx := context.Background()
	var satelliteID int
	require.NoError(t, db.QueryRowContext(ctx, "INSERT INTO satellites (name) VALUES ('edge-01') RETURNING id").Scan(&satelliteID))
	for i := range 2 {
		_, err := db.ExecContext(ctx,
			"INSERT INTO robot_accounts (robot_name, robot_secret_hash, robot_id, satellite_id) VALUES ($1, 'hash', $2, $3)",
			fmt.Sprintf("robot$edge-01-%d", i), fmt.Sprintf("%d", 100+i), satelliteID)
		require.NoError(t, err)
	}
}

// withPendingConstraints is the embedded schema plus sql/pending/constraints.sql
// as the next migration, which is where 3.4 puts it.
func withPendingConstraints(t *testing.T) (fs.FS, int64) {
	t.Helper()
	latest, err := dbtest.LatestMigration()
	require.NoError(t, err)
	pendingSQL, err := os.ReadFile("pending/constraints.sql")
	require.NoError(t, err)

	files := fstest.MapFS{}
	entries, err := fs.ReadDir(migrations.Schema, "schema")
	require.NoError(t, err)
	for _, e := range entries {
		data, err := fs.ReadFile(migrations.Schema, "schema/"+e.Name())
		require.NoError(t, err)
		files[e.Name()] = &fstest.MapFile{Data: data}
	}
	files[fmt.Sprintf("%03d_constraints.sql", latest+1)] = &fstest.MapFile{Data: pendingSQL}
	return files, latest
}

func columnType(t *testing.T, db *sql.DB, table, column string) string {
	t.Helper()
	var dataType string
	require.NoError(t, db.QueryRowContext(context.Background(),
		"SELECT data_type FROM information_schema.columns WHERE table_schema = 'public' AND table_name = $1 AND column_name = $2",
		table, column).Scan(&dataType))
	return dataType
}

// The pending DDL is not applied anywhere, so this keeps it valid until it
// moves into sql/schema.
func TestPendingConstraintsSQL_AppliesRefusesDuplicatesAndReverts(t *testing.T) {
	db := dbtest.New(t)
	ctx := context.Background()
	schema, latest := withPendingConstraints(t)
	p := provider(t, db.DB, schema)
	insertDuplicateRobots(t, db.DB)

	_, err := p.Up(ctx)
	require.ErrorContains(t, err, "robot_accounts has several robots for satellites")
	version, err := p.GetDBVersion(ctx)
	require.NoError(t, err)
	require.Equal(t, latest, version)

	_, err = db.ExecContext(ctx, "DELETE FROM robot_accounts WHERE robot_id = '101'")
	require.NoError(t, err)
	_, err = p.Up(ctx)
	require.NoError(t, err)
	require.Equal(t, "timestamp with time zone", columnType(t, db.DB, "satellites", "last_seen"))
	require.Equal(t, "integer", columnType(t, db.DB, "configs", "version"))

	_, err = p.DownTo(ctx, latest)
	require.NoError(t, err)
	require.Equal(t, "timestamp without time zone", columnType(t, db.DB, "satellites", "last_seen"))
	var versionColumns int
	require.NoError(t, db.QueryRowContext(ctx,
		"SELECT count(*) FROM information_schema.columns WHERE table_name = 'configs' AND column_name = 'version'").Scan(&versionColumns))
	require.Zero(t, versionColumns)
}

func TestConstraintsMigration_FailsOnDuplicateRobots(t *testing.T) {
	pending.Plan(t, "3.4", "robot_accounts allows several robots per satellite")
	db := dbtest.New(t)
	ctx := context.Background()
	schema := embeddedSchema(t)

	entries, err := fs.ReadDir(schema, ".")
	require.NoError(t, err)
	var constraintsVersion int64
	for _, e := range entries {
		data, err := fs.ReadFile(schema, e.Name())
		require.NoError(t, err)
		if bytes.Contains(data, []byte("robot_accounts_satellite_id_key")) {
			_, err := fmt.Sscanf(e.Name(), "%d_", &constraintsVersion)
			require.NoError(t, err)
		}
	}
	require.NotZero(t, constraintsVersion, "no embedded migration adds robot_accounts_satellite_id_key")

	p := provider(t, db.DB, schema)
	_, err = p.DownTo(ctx, constraintsVersion-1)
	require.NoError(t, err)
	insertDuplicateRobots(t, db.DB)

	_, err = p.Up(ctx)
	require.ErrorContains(t, err, "robot_accounts has several robots for satellites")
}
