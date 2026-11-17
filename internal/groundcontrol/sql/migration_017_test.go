package migrations_test

import (
	"context"
	"testing"

	"github.com/container-registry/harbor-satellite/internal/groundcontrol/dbtest"
	"github.com/stretchr/testify/require"
)

func TestMigration017_DownAndUp(t *testing.T) {
	db := dbtest.New(t)
	ctx := context.Background()
	p := provider(t, db.DB, embeddedSchema(t))
	require.Subset(t, publicTables(t, db.DB), []string{"desired_state", "event_outbox"})

	_, err := p.DownTo(ctx, 16)
	require.NoError(t, err)
	require.NotContains(t, publicTables(t, db.DB), "event_outbox")
	require.NotContains(t, publicTables(t, db.DB), "desired_state")

	_, err = p.Up(ctx)
	require.NoError(t, err)
	require.Subset(t, publicTables(t, db.DB), []string{"desired_state", "event_outbox"})
}
