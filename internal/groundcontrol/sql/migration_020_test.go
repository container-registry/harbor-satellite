package migrations_test

import (
	"context"
	"testing"

	"github.com/container-registry/harbor-satellite/internal/groundcontrol/dbtest"
	"github.com/stretchr/testify/require"
)

func TestMigration020_DownUp(t *testing.T) {
	db := dbtest.New(t)
	ctx := context.Background()
	p := provider(t, db.DB, embeddedSchema(t))
	policyTables := []string{"policy_role_bindings", "policy_rules", "policy_version"}
	require.Subset(t, publicTables(t, db.DB), policyTables)

	_, err := p.DownTo(ctx, 19)
	require.NoError(t, err)
	for _, table := range policyTables {
		require.NotContains(t, publicTables(t, db.DB), table)
	}

	_, err = p.Up(ctx)
	require.NoError(t, err)
	require.Subset(t, publicTables(t, db.DB), policyTables)
	var version int64
	require.NoError(t, db.QueryRowContext(ctx, "SELECT version FROM policy_version").Scan(&version))
	require.Zero(t, version, "Up seeds the single version row again")
	_, err = db.ExecContext(ctx, "INSERT INTO policy_version (singleton) VALUES (FALSE)")
	require.Error(t, err, "policy_version holds one row")
}
