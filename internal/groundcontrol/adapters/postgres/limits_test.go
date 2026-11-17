package postgres_test

import (
	"context"
	"math"
	"testing"
	"time"

	"github.com/container-registry/harbor-satellite/internal/groundcontrol/adapters/postgres"
	"github.com/container-registry/harbor-satellite/internal/groundcontrol/dbtest"
	"github.com/container-registry/harbor-satellite/internal/groundcontrol/webhook"
	"github.com/stretchr/testify/require"
)

// WEBHOOK_BATCH_SIZE and the outbox batch sizes are operator input; a value
// above the INT range must not wrap into a negative LIMIT.
func TestStores_ClampBatchSizesAboveTheIntRange(t *testing.T) {
	db := dbtest.New(t)
	ctx := context.Background()
	huge := math.MaxInt32 + 1
	webhooks, err := postgres.NewWebhookStore(db.DB)
	require.NoError(t, err)
	_, err = webhooks.Due(ctx, huge)
	require.NoError(t, err, "Due")
	log, err := postgres.NewOutboxLog(db.DB)
	require.NoError(t, err)
	_, err = log.ReadAfter(ctx, webhook.Position{}, huge)
	require.NoError(t, err, "ReadAfter")
	outbox, err := postgres.NewOutboxStore(db.DB)
	require.NoError(t, err)
	_, err = outbox.Prune(ctx, time.Hour, huge)
	require.NoError(t, err, "Prune")
	rotations, err := postgres.NewRotationStore(db.DB)
	require.NoError(t, err)
	_, err = rotations.ExpiringBefore(ctx, time.Now(), time.Now(), huge)
	require.NoError(t, err, "ExpiringBefore")
	_, err = rotations.Acked(ctx, huge)
	require.NoError(t, err, "Acked")
}
