package migrations_test

import (
	"context"
	"testing"

	"github.com/container-registry/harbor-satellite/internal/groundcontrol/dbtest"
	"github.com/stretchr/testify/require"
)

func TestMigration022_DownUp(t *testing.T) {
	db := dbtest.New(t)
	ctx := context.Background()
	p := provider(t, db.DB, embeddedSchema(t))
	tables := []string{"webhook_checkpoints", "webhook_subscriptions"}
	index := func() bool {
		t.Helper()
		var n int
		require.NoError(t, db.QueryRowContext(ctx, "SELECT count(*) FROM pg_indexes WHERE indexname = 'idx_event_outbox_xid_id'").Scan(&n))
		return n == 1
	}
	require.Subset(t, publicTables(t, db.DB), tables)
	require.True(t, index())

	_, err := p.DownTo(ctx, 21)
	require.NoError(t, err)
	for _, table := range tables {
		require.NotContains(t, publicTables(t, db.DB), table)
	}
	require.False(t, index())

	_, err = p.Up(ctx)
	require.NoError(t, err)
	require.Subset(t, publicTables(t, db.DB), tables)
	require.True(t, index())
}
