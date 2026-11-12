package migrations_test

import (
	"context"
	"testing"

	"github.com/container-registry/harbor-satellite/internal/groundcontrol/dbtest"
	"github.com/stretchr/testify/require"
)

func TestMigration019_DownUp(t *testing.T) {
	db := dbtest.New(t)
	ctx := context.Background()
	p := provider(t, db.DB, embeddedSchema(t))
	tables := []string{"satellite_identities", "satellite_sessions", "satellite_view"}
	require.Subset(t, publicTables(t, db.DB), tables)

	_, err := p.DownTo(ctx, 18)
	require.NoError(t, err)
	for _, table := range tables {
		require.NotContains(t, publicTables(t, db.DB), table)
	}
	require.Contains(t, publicTables(t, db.DB), "satellite_journal_cursor", "018 stays")

	_, err = p.Up(ctx)
	require.NoError(t, err)
	require.Subset(t, publicTables(t, db.DB), tables)
}
