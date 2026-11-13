package app

import (
	"context"
	"testing"

	"github.com/container-registry/harbor-satellite/internal/groundcontrol/database"
	"github.com/container-registry/harbor-satellite/internal/groundcontrol/dbpool"
	"github.com/container-registry/harbor-satellite/internal/groundcontrol/dbtest"
	"github.com/container-registry/harbor-satellite/internal/groundcontrol/fleet"
	"github.com/container-registry/harbor-satellite/internal/groundcontrol/harbortest"
	"github.com/container-registry/harbor-satellite/internal/shared/env"
	"github.com/stretchr/testify/require"
)

// OUTBOX_MAX_ATTEMPTS 0 means the publisher's default for
// /api/ops/outbox?failed=true too, so a pending event is not listed as
// failed.
func TestServerOptions_OutboxReaderUsesThePublisherDefault(t *testing.T) {
	db := dbtest.New(t)
	ctx := context.Background()
	stub := harbortest.New(t)
	pools := &dbpool.Pools{API: db.DB, Fleet: db.DB, Async: db.DB}
	f, err := NewFleet(FleetDeps{
		Pools:  pools,
		Harbor: env.Harbor{URL: stub.URL(), Username: harbortest.Username, Password: harbortest.Password, RobotDurationDays: "30"},
	})
	require.NoError(t, err)
	_, err = database.New(db.DB).AppendOutboxEvent(ctx, database.AppendOutboxEventParams{
		Kind: string(fleet.EventGroupChanged), AggregateType: "group", AggregateID: 1, Payload: []byte(`{"name":"g1"}`),
	})
	require.NoError(t, err)
	reader, ok := f.outboxReader(pools)
	require.True(t, ok)

	pending, err := reader.ListOutbox(ctx, false, 10)
	require.NoError(t, err)
	require.Len(t, pending, 1)
	failed, err := reader.ListOutbox(ctx, true, 10)
	require.NoError(t, err)
	require.Empty(t, failed, "an event with no attempts is pending, not failed")
}
