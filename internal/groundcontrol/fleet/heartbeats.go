package fleet

import (
	"context"
	"time"

	"github.com/container-registry/harbor-satellite/internal/shared/inventory"
	"github.com/container-registry/harbor-satellite/internal/shared/notimpl"
)

// InventoryItem is one cached image a satellite reports (plan item 4.4).
type InventoryItem = inventory.Item

// StatusSample is one satellite_status row.
type StatusSample struct {
	Satellite                                           SatelliteID
	Activity, LatestStateDigest, LatestConfigDigest     string
	CPUPercent                                          float64
	MemoryUsedBytes, StorageUsedBytes, LastSyncDuration int64
	ImageCount                                          int32
	ReportedAt                                          time.Time // request_created_time, as today
}

// Heartbeat is what a beat leaves for the flusher (plan item 4.2).
type Heartbeat struct {
	Satellite                 SatelliteID
	SeenAt                    time.Time
	Interval                  string // normalized "@every …"
	StateDigest, ConfigDigest Digest
	AppliedStates             []AppliedState
	LastReplicationError      string
	InventoryDigest           Digest
	Inventory                 []InventoryItem // nil unless the inventory changed
	Sample                    StatusSample
}

// HeartbeatSink buffers heartbeats per replica; heartbeat.Buffer is the
// implementation.
type HeartbeatSink interface {
	Record(beat Heartbeat)
	// InventoryDigest is the newest digest this replica buffered for the
	// satellite, so a beat compares with it before the flush.
	InventoryDigest(satellite SatelliteID) (Digest, bool)
}

type HeartbeatCmd struct {
	Auth                      SatelliteAuth
	Interval                  string
	Sample                    StatusSample
	StateDigest, ConfigDigest Digest
	AppliedStates             []AppliedState
	LastReplicationError      string
	InventoryDigest           Digest          // empty from satellites before 4.4: GC computes it
	Inventory                 []InventoryItem // nil when none was sent
	Journal                   *JournalUpload  // nil when the beat carries no journal
}

type HeartbeatResult struct {
	Events             []string // "refresh_credentials"
	InventoryRequested bool
	Journal            *JournalAck // set whenever cmd.Journal is set, error included
	Session            *SessionGrant
}

// HeartbeatService takes satellite heartbeats on the fleet plane; it owns
// the JournalService.
type HeartbeatService struct{}

// Beat validates the report against the column limits of 008 and 015
// (activity and digests at most 255 bytes, references at most 512, cpu in
// [0, 999.99], counts >= 0) and answers ErrInvalid (400) for a bad report.
// The interval is recorded normalized ("@every HHhMMmSSs"), the only form
// MarkStaleSatellites reads.
// It records the heartbeat in the sink first and only then calls
// JournalService.Ingest, so a malformed journal never makes a satellite
// stale: an ErrInvalid from Ingest becomes Journal with Error set, the
// cursor unchanged, and the heartbeat still answers 200.
//
// Statements:
//   - none for a plain beat (cached auth, buffered write);
//   - at most 4 for a journal (Ingest);
//   - 1 more for a session renewal, so a renewing journaled beat is at most 5;
//   - for an AuthRobot beat without HasSession, IssueIfNone once, its grant in Session.
//
// An inventory digest that differs from the buffered or stored one without
// an inventory sets InventoryRequested; a legacy beat with an inventory
// but no digest gets the digest computed. Revoked is ErrRevoked.
// refresh_credentials comes from Auth.RobotExpiresAt with today's rule.
func (s *HeartbeatService) Beat(ctx context.Context, cmd HeartbeatCmd) (HeartbeatResult, error) {
	return HeartbeatResult{}, notimpl.Err("4.2 heartbeat")
}
