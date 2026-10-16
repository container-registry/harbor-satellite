package migrations_test

import (
	"context"
	"testing"

	"github.com/container-registry/harbor-satellite/internal/groundcontrol/dbtest"
	"github.com/stretchr/testify/require"
)

func TestMigration021_DownUp(t *testing.T) {
	db := dbtest.New(t)
	ctx := context.Background()
	p := provider(t, db.DB, embeddedSchema(t))
	tables := []string{"oidc_login_states", "robot_rotations", "user_identities"}
	require.Subset(t, publicTables(t, db.DB), tables)

	_, err := p.DownTo(ctx, 20)
	require.NoError(t, err)
	for _, table := range tables {
		require.NotContains(t, publicTables(t, db.DB), table)
	}

	_, err = p.Up(ctx)
	require.NoError(t, err)
	require.Subset(t, publicTables(t, db.DB), tables)
}
