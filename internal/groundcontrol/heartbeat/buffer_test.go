package heartbeat_test

import (
	"testing"
	"time"

	"github.com/container-registry/harbor-satellite/internal/groundcontrol/fleet"
	"github.com/container-registry/harbor-satellite/internal/groundcontrol/heartbeat"
	"github.com/container-registry/harbor-satellite/internal/shared/testutil/pending"
	"github.com/stretchr/testify/require"
)

const pendingBuffer = "the heartbeat writes 8 statements in the request; there is no buffer"

var cfg = heartbeat.Config{FlushInterval: 5 * time.Second, MaxPending: 100, MaxFlushFailures: 3, FinalFlushTimeout: time.Second}

func newBuffer(t *testing.T, c heartbeat.Config) *heartbeat.Buffer {
	t.Helper()
	b, err := heartbeat.NewBuffer(c)
	require.NoError(t, err)
	return b
}

func TestBuffer_LatestBySeenAtKeepsAllSamples(t *testing.T) {
	pending.Plan(t, "4.2", pendingBuffer)
	b := newBuffer(t, cfg)
	b.Record(beatAt(7, 2*time.Second))
	b.Record(beatAt(7, time.Second)) // arrives late, older
	b.Record(beatAt(8, 0))

	batch := b.Drain()

	require.Len(t, batch.Latest, 2)
	latest := map[fleet.SatelliteID]time.Time{}
	for _, beat := range batch.Latest {
		latest[beat.Satellite] = beat.SeenAt
	}
	require.Equal(t, start.Add(2*time.Second), latest[7], "a late older beat never wins")
	require.Len(t, batch.Samples, 3, "every sample is kept for satellite_status")
	require.Empty(t, b.Drain().Latest, "drain empties the buffer")
}

func TestBuffer_KeepsNewestNonNilInventory(t *testing.T) {
	pending.Plan(t, "4.4", pendingBuffer)
	b := newBuffer(t, cfg)
	withInventory := beatAt(7, 0)
	withInventory.InventoryDigest = "sha256:inv"
	withInventory.Inventory = []fleet.InventoryItem{{Reference: "library/alpine:3.20", SizeBytes: 3 << 20}}
	plain := beatAt(7, time.Second)
	plain.InventoryDigest = "sha256:inv"
	b.Record(withInventory)
	b.Record(plain)

	digest, ok := b.InventoryDigest(7)
	require.True(t, ok)
	require.Equal(t, fleet.Digest("sha256:inv"), digest)
	batch := b.Drain()

	require.Len(t, batch.Latest, 1)
	require.Equal(t, start.Add(time.Second), batch.Latest[0].SeenAt)
	require.Equal(t, []heartbeat.InventoryUpdate{{Satellite: 7, Digest: "sha256:inv", Items: withInventory.Inventory}}, batch.Inventories)
}

func TestBuffer_CoalescesAtMaxPending(t *testing.T) {
	pending.Plan(t, "4.2", pendingBuffer)
	c := cfg
	c.MaxPending = 3
	b := newBuffer(t, c)
	for i := range 10 {
		b.Record(beatAt(7, time.Duration(i)*time.Second))
	}

	batch := b.Drain()

	require.LessOrEqual(t, len(batch.Samples), 3)
	require.Len(t, batch.Latest, 1)
	require.Equal(t, start.Add(9*time.Second), batch.Latest[0].SeenAt, "the newest sample survives")
}

func TestBuffer_RequeueMergesBySeenAt(t *testing.T) {
	pending.Plan(t, "4.2", pendingBuffer)
	b := newBuffer(t, cfg)
	b.Record(beatAt(7, 0))
	b.Record(beatAt(8, 0))
	failed := b.Drain()
	b.Record(beatAt(7, 5*time.Second))

	b.Requeue(failed)
	batch := b.Drain()

	latest := map[fleet.SatelliteID]time.Time{}
	for _, beat := range batch.Latest {
		latest[beat.Satellite] = beat.SeenAt
	}
	require.Equal(t, start.Add(5*time.Second), latest[7], "a beat recorded meanwhile wins")
	require.Equal(t, start, latest[8], "the requeued beat comes back")
	require.Len(t, batch.Samples, 3)
}
