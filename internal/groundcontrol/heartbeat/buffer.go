package heartbeat

import (
	"context"
	"time"

	"github.com/container-registry/harbor-satellite/internal/groundcontrol/fleet"
	"github.com/container-registry/harbor-satellite/internal/shared/notimpl"
)

type Config struct {
	FlushInterval     time.Duration
	MaxPending        int           // beyond it a satellite's older samples coalesce into its newest
	MaxFlushFailures  int           // a satellite's beats are dropped, with an error log, after this many failed flushes
	FinalFlushTimeout time.Duration // the flush after ctx ends runs on context.WithoutCancel with this timeout
}

type InventoryUpdate struct {
	Satellite fleet.SatelliteID
	Digest    fleet.Digest
	Items     []fleet.InventoryItem
}

type Batch struct {
	Latest      []fleet.Heartbeat // newest per satellite by SeenAt
	Samples     []fleet.StatusSample
	Inventories []InventoryUpdate // newest non-nil inventory per satellite, kept apart from Latest
}

type Store interface {
	// Flush runs FlushLastSeen, FlushSatelliteView, InsertSatelliteStatusBatch
	// and, when any, StoreSatelliteInventories in one transaction. Latest
	// and Inventories hold at most one entry per satellite; a duplicate
	// fails the whole flush.
	Flush(ctx context.Context, batch Batch) error
}

// Buffer holds this replica's beats until the next flush; it is safe for
// concurrent use.
type Buffer struct{}

var _ fleet.HeartbeatSink = (*Buffer)(nil)

func NewBuffer(cfg Config) (*Buffer, error) {
	return nil, notimpl.Err("4.2 heartbeat buffer")
}

func (b *Buffer) Record(beat fleet.Heartbeat) {}

func (b *Buffer) InventoryDigest(satellite fleet.SatelliteID) (fleet.Digest, bool) { return "", false }

// Drain hands out everything buffered and empties the buffer.
func (b *Buffer) Drain() Batch { return Batch{} }

// Requeue puts a failed batch back, merging by SeenAt so newer beats recorded meanwhile win.
func (b *Buffer) Requeue(batch Batch) {}
