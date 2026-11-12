package heartbeat

import (
	"context"
	"fmt"
	"slices"
	"sync"
	"time"

	"github.com/container-registry/harbor-satellite/internal/groundcontrol/fleet"
)

type Config struct {
	FlushInterval     time.Duration
	MaxPending        int           // beyond it a satellite's older samples coalesce into its newest
	MaxFlushFailures  int           // a satellite's beats are dropped, with an error log, after this many failed flushes
	FinalFlushTimeout time.Duration // the flush after ctx ends runs on context.WithoutCancel with this timeout
}

func (c Config) validate() error {
	if c.FlushInterval <= 0 || c.MaxPending <= 0 || c.MaxFlushFailures <= 0 || c.FinalFlushTimeout <= 0 {
		return fmt.Errorf("%w: heartbeat buffer needs a positive flush interval, max pending, max flush failures and final flush timeout", fleet.ErrInvalid)
	}
	return nil
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

func (b Batch) empty() bool {
	return len(b.Latest) == 0 && len(b.Samples) == 0 && len(b.Inventories) == 0
}

type Store interface {
	// Flush runs FlushLastSeen, FlushSatelliteView, InsertSatelliteStatusBatch
	// and, when any, StoreSatelliteInventories in one transaction. Latest
	// and Inventories hold at most one entry per satellite; a duplicate
	// fails the whole flush.
	Flush(ctx context.Context, batch Batch) error
}

type pending struct {
	latest       fleet.Heartbeat
	samples      []fleet.StatusSample
	inventory    *InventoryUpdate
	inventoryAt  time.Time
	hasHeartbeat bool
}

// Buffer holds this replica's beats until the next flush; it is safe for
// concurrent use.
type Buffer struct {
	cfg     Config
	mu      sync.Mutex
	entries map[fleet.SatelliteID]*pending
	// digests outlive a drain: until the satellite's cached auth expires,
	// they are the only place this replica knows the flushed digest from.
	digests map[fleet.SatelliteID]fleet.Digest
}

var _ fleet.HeartbeatSink = (*Buffer)(nil)

func NewBuffer(cfg Config) (*Buffer, error) {
	if err := cfg.validate(); err != nil {
		return nil, err
	}
	return &Buffer{cfg: cfg, entries: map[fleet.SatelliteID]*pending{}, digests: map[fleet.SatelliteID]fleet.Digest{}}, nil
}

func (b *Buffer) Record(beat fleet.Heartbeat) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.add(beat.Satellite, &beat, []fleet.StatusSample{beat.Sample}, inventoryOf(beat), beat.SeenAt, false)
	if beat.InventoryDigest != "" {
		b.digests[beat.Satellite] = beat.InventoryDigest
	}
}

func inventoryOf(beat fleet.Heartbeat) *InventoryUpdate {
	if beat.Inventory == nil {
		return nil
	}
	return &InventoryUpdate{Satellite: beat.Satellite, Digest: beat.InventoryDigest, Items: beat.Inventory}
}

// add merges one satellite's beat, samples and inventory; older lands
// before what is buffered, as a requeued batch does.
func (b *Buffer) add(satellite fleet.SatelliteID, beat *fleet.Heartbeat, samples []fleet.StatusSample, inv *InventoryUpdate, invAt time.Time, older bool) {
	e := b.entries[satellite]
	if e == nil {
		e = &pending{}
		b.entries[satellite] = e
	}
	if beat != nil && (!e.hasHeartbeat || !beat.SeenAt.Before(e.latest.SeenAt)) {
		e.latest, e.hasHeartbeat = *beat, true
	}
	if older {
		e.samples = append(slices.Clone(samples), e.samples...)
	} else {
		e.samples = append(e.samples, samples...)
	}
	if over := len(e.samples) - b.cfg.MaxPending; over > 0 {
		e.samples = slices.Delete(e.samples, 0, over)
	}
	if inv != nil && (e.inventory == nil || !invAt.Before(e.inventoryAt)) {
		e.inventory, e.inventoryAt = inv, invAt
	}
}

func (b *Buffer) InventoryDigest(satellite fleet.SatelliteID) (fleet.Digest, bool) {
	b.mu.Lock()
	defer b.mu.Unlock()
	d, ok := b.digests[satellite]
	return d, ok
}

// Drain hands out everything buffered and empties the buffer.
func (b *Buffer) Drain() Batch {
	b.mu.Lock()
	defer b.mu.Unlock()
	ids := make([]fleet.SatelliteID, 0, len(b.entries))
	for id := range b.entries {
		ids = append(ids, id)
	}
	slices.Sort(ids)
	var batch Batch
	for _, id := range ids {
		e := b.entries[id]
		if e.hasHeartbeat {
			batch.Latest = append(batch.Latest, e.latest)
		}
		batch.Samples = append(batch.Samples, e.samples...)
		if e.inventory != nil {
			batch.Inventories = append(batch.Inventories, *e.inventory)
		}
	}
	b.entries = map[fleet.SatelliteID]*pending{}
	return batch
}

// Requeue puts a failed batch back, merging by SeenAt so newer beats recorded meanwhile win.
func (b *Buffer) Requeue(batch Batch) {
	b.mu.Lock()
	defer b.mu.Unlock()
	for _, part := range split(batch) {
		var beat *fleet.Heartbeat
		invAt := time.Time{}
		if len(part.batch.Latest) > 0 {
			beat = &part.batch.Latest[0]
			invAt = beat.SeenAt
		}
		b.add(part.satellite, beat, part.batch.Samples, firstInventory(part.batch), invAt, true)
	}
}

type satelliteBatch struct {
	satellite fleet.SatelliteID
	batch     Batch
}

// split cuts a batch into one batch per satellite, in satellite order.
func split(batch Batch) []satelliteBatch {
	parts := map[fleet.SatelliteID]*satelliteBatch{}
	part := func(id fleet.SatelliteID) *satelliteBatch {
		if parts[id] == nil {
			parts[id] = &satelliteBatch{satellite: id}
		}
		return parts[id]
	}
	for _, beat := range batch.Latest {
		p := part(beat.Satellite)
		p.batch.Latest = append(p.batch.Latest, beat)
	}
	for _, s := range batch.Samples {
		p := part(s.Satellite)
		p.batch.Samples = append(p.batch.Samples, s)
	}
	for _, inv := range batch.Inventories {
		p := part(inv.Satellite)
		p.batch.Inventories = append(p.batch.Inventories, inv)
	}
	out := make([]satelliteBatch, 0, len(parts))
	for _, p := range parts {
		out = append(out, *p)
	}
	slices.SortFunc(out, func(a, b satelliteBatch) int { return int(a.satellite) - int(b.satellite) })
	return out
}

func firstInventory(b Batch) *InventoryUpdate {
	if len(b.Inventories) == 0 {
		return nil
	}
	inv := b.Inventories[0]
	return &inv
}
