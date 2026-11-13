package postgres

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/container-registry/harbor-satellite/internal/groundcontrol/database"
	"github.com/container-registry/harbor-satellite/internal/groundcontrol/fleet"
	"github.com/container-registry/harbor-satellite/internal/groundcontrol/heartbeat"
)

var _ heartbeat.Store = (*HeartbeatStore)(nil)

// HeartbeatStore writes a heartbeat batch in one transaction on the fleet
// plane's pool: FlushLastSeen, FlushSatelliteView,
// InsertSatelliteStatusBatch and, when the batch has any,
// StoreSatelliteInventories and AckRobotRotations, each with the whole batch as one JSON
// argument. The statements read times as TIMESTAMPTZ, so
// satellites.last_seen and satellite_status.reported_at, which are
// TIMESTAMP, get the session time zone's wall clock, as NOW() writes them.
type HeartbeatStore struct {
	db    *sql.DB
	q     *database.Queries
	clock fleet.Clock
}

func NewHeartbeatStore(db *sql.DB, clock fleet.Clock) (*HeartbeatStore, error) {
	if db == nil {
		return nil, fmt.Errorf("%w: the heartbeat store needs a database", fleet.ErrInvalid)
	}
	if clock == nil {
		clock = fleet.SystemClock{}
	}
	return &HeartbeatStore{db: db, q: database.New(db), clock: clock}, nil
}

// Ping checks that the fleet pool reaches PostgreSQL.
func (s *HeartbeatStore) Ping(ctx context.Context) error { return s.db.PingContext(ctx) }

type lastSeenRow struct {
	ID                int32     `json:"id"`
	SeenAt            time.Time `json:"seen_at"`
	HeartbeatInterval string    `json:"heartbeat_interval"`
}

type viewRow struct {
	ID                   int32                `json:"id"`
	SeenAt               time.Time            `json:"seen_at"`
	HeartbeatInterval    string               `json:"heartbeat_interval"`
	Activity             string               `json:"activity"`
	ReportedAt           *time.Time           `json:"reported_at"`
	StateDigest          string               `json:"state_digest"`
	ConfigDigest         string               `json:"config_digest"`
	AppliedStates        []fleet.AppliedState `json:"applied_states"`
	InventoryDigest      string               `json:"inventory_digest"`
	LastReplicationError string               `json:"last_replication_error"`
}

type statusRow struct {
	SatelliteID            int32       `json:"satellite_id"`
	Activity               string      `json:"activity"`
	LatestStateDigest      *string     `json:"latest_state_digest"`
	LatestConfigDigest     *string     `json:"latest_config_digest"`
	CPUPercent             json.Number `json:"cpu_percent"`
	MemoryUsedBytes        int64       `json:"memory_used_bytes"`
	StorageUsedBytes       int64       `json:"storage_used_bytes"`
	LastSyncDurationMs     int64       `json:"last_sync_duration_ms"`
	ImageCount             int32       `json:"image_count"`
	ReportedAt             *time.Time  `json:"reported_at"`
	DownloadBytesPerSecond float64     `json:"download_bytes_per_second"`
	BytesInWindow          int64       `json:"bytes_in_window"`
	BytesTotal             int64       `json:"bytes_total"`
}

type inventoryItemRow struct {
	Reference string `json:"reference"`
	SizeBytes int64  `json:"size_bytes"`
}

type inventoryRow struct {
	SatelliteID int32              `json:"satellite_id"`
	Digest      string             `json:"digest"`
	Items       []inventoryItemRow `json:"items"`
}

type ackRow struct {
	SatelliteID int32 `json:"satellite_id"`
	Generation  int64 `json:"generation"`
}

func nonEmpty(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

func timeOrNil(t time.Time) *time.Time {
	if t.IsZero() {
		return nil
	}
	return &t
}

func (s *HeartbeatStore) Flush(ctx context.Context, batch heartbeat.Batch) (err error) {
	seen := make([]lastSeenRow, 0, len(batch.Latest))
	views := make([]viewRow, 0, len(batch.Latest))
	var acks []ackRow
	for _, b := range batch.Latest {
		seen = append(seen, lastSeenRow{ID: int32(b.Satellite), SeenAt: b.SeenAt, HeartbeatInterval: b.Interval})
		applied := b.AppliedStates
		if applied == nil {
			applied = []fleet.AppliedState{}
		}
		views = append(views, viewRow{
			ID: int32(b.Satellite), SeenAt: b.SeenAt, HeartbeatInterval: b.Interval, Activity: b.Sample.Activity,
			ReportedAt: timeOrNil(b.Sample.ReportedAt), StateDigest: string(b.StateDigest), ConfigDigest: string(b.ConfigDigest),
			AppliedStates: applied, InventoryDigest: string(b.InventoryDigest), LastReplicationError: b.LastReplicationError,
		})
		if b.CredentialsGeneration > 0 {
			acks = append(acks, ackRow{SatelliteID: int32(b.Satellite), Generation: b.CredentialsGeneration})
		}
	}
	samples := make([]statusRow, 0, len(batch.Samples))
	for _, sm := range batch.Samples {
		samples = append(samples, statusRow{
			SatelliteID: int32(sm.Satellite), Activity: sm.Activity,
			LatestStateDigest: nonEmpty(sm.LatestStateDigest), LatestConfigDigest: nonEmpty(sm.LatestConfigDigest),
			CPUPercent:      json.Number(strconv.FormatFloat(sm.CPUPercent, 'f', 2, 64)),
			MemoryUsedBytes: sm.MemoryUsedBytes, StorageUsedBytes: sm.StorageUsedBytes, LastSyncDurationMs: sm.LastSyncDuration,
			ImageCount: sm.ImageCount, ReportedAt: timeOrNil(sm.ReportedAt),
			DownloadBytesPerSecond: sm.DownloadBytesPerSecond, BytesInWindow: sm.BytesInWindow, BytesTotal: sm.BytesTotal,
		})
	}
	inventories := make([]inventoryRow, 0, len(batch.Inventories))
	for _, inv := range batch.Inventories {
		items := make([]inventoryItemRow, 0, len(inv.Items))
		for _, it := range inv.Items {
			items = append(items, inventoryItemRow{Reference: it.Reference, SizeBytes: it.SizeBytes})
		}
		inventories = append(inventories, inventoryRow{SatelliteID: int32(inv.Satellite), Digest: string(inv.Digest), Items: items})
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin heartbeat flush: %w", err)
	}
	defer func() {
		if err == nil {
			return
		}
		if rbErr := tx.Rollback(); rbErr != nil && !errors.Is(rbErr, sql.ErrTxDone) {
			err = errors.Join(err, rbErr)
		}
	}()
	q := s.q.WithTx(tx)
	now := s.clock.Now()
	if len(seen) > 0 {
		if err := q.FlushLastSeen(ctx, mustJSON(seen)); err != nil {
			return fmt.Errorf("flush last_seen: %w", mapError(err))
		}
		if err := q.FlushSatelliteView(ctx, database.FlushSatelliteViewParams{Now: now, Rows: mustJSON(views)}); err != nil {
			return fmt.Errorf("flush satellite_view: %w", mapError(err))
		}
	}
	if len(samples) > 0 {
		if err := q.InsertSatelliteStatusBatch(ctx, mustJSON(samples)); err != nil {
			return fmt.Errorf("insert status history: %w", mapError(err))
		}
	}
	if len(inventories) > 0 {
		if err := q.StoreSatelliteInventories(ctx, database.StoreSatelliteInventoriesParams{Now: now, Rows: mustJSON(inventories)}); err != nil {
			return fmt.Errorf("store inventories: %w", mapError(err))
		}
	}
	if len(acks) > 0 {
		if err := q.AckRobotRotations(ctx, database.AckRobotRotationsParams{Now: now, Acks: mustJSON(acks)}); err != nil {
			return fmt.Errorf("ack robot rotations: %w", mapError(err))
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit heartbeat flush: %w", err)
	}
	return nil
}

// mustJSON marshals rows built from plain fields, which cannot fail.
func mustJSON(v any) json.RawMessage {
	b, err := json.Marshal(v)
	if err != nil {
		panic(fmt.Sprintf("marshal heartbeat rows: %v", err))
	}
	return b
}
