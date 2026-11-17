package fleet

import (
	"context"
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/container-registry/harbor-satellite/internal/shared/inventory"
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
	// CredentialsGeneration acks a delivered rotation (plan item 8.4). Beat
	// sets it only when the satellite's generation equals its pending
	// rotation's, so the flush runs AckRobotRotations only while an ack is
	// due, not for every satellite that reports its current generation.
	CredentialsGeneration int64
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
	CredentialsGeneration     int64           // the robot generation the satellite uses (plan item 8.4)
}

type HeartbeatResult struct {
	Events             []string // "refresh_credentials"
	InventoryRequested bool
	// Journal answers cmd.Journal, with Error for a malformed upload. It
	// is nil when the satellite sent none or when ingesting failed.
	Journal *JournalAck
	// JournalErr is why ingesting cmd.Journal failed, for the caller to
	// log; the beat itself still succeeds.
	JournalErr error
	Session    *SessionGrant
	// Credentials is a pending rotation, delivered only to session and SVID
	// beats whose CredentialsGeneration is older (plan item 8.4): a robot
	// Basic satellite would switch to a robot Ground Control does not yet
	// accept for Basic auth.
	Credentials *RotatedCredentials
}

// HeartbeatService takes satellite heartbeats on the fleet plane; it owns
// the JournalService.
type HeartbeatService struct {
	c         *core
	journal   *JournalService
	sink      HeartbeatSink
	rotations RotationSource
}

// Beat validates the report against the column limits of 008 and 015
// (activity and digests at most 255 bytes, references at most 512, cpu in
// [0, 999.99], counts >= 0) and answers ErrInvalid (400) for a bad report.
// The interval is recorded normalized ("@every HHhMMmSSs"), the only form
// MarkStaleSatellites reads.
// It records the heartbeat in the sink first and only then calls
// JournalService.Ingest, so a journal never makes a satellite stale nor
// fails the beat: a malformed upload is answered in Journal with Error set
// and the cursor unchanged, and a failing repository leaves Journal nil
// and sets JournalErr. Either way the session renewal, a pending rotation
// and the events still reach the satellite.
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
	auth := cmd.Auth
	if auth.State == StateRevoked {
		return HeartbeatResult{}, ErrRevoked
	}
	if err := validateBeat(cmd); err != nil {
		return HeartbeatResult{}, err
	}
	interval, every, err := normalizeInterval(cmd.Interval)
	if err != nil {
		return HeartbeatResult{}, err
	}
	now := s.c.clock.Now()
	res := HeartbeatResult{Events: refreshEvents(auth.RobotExpiresAt, every, now)}

	digest := cmd.InventoryDigest
	switch {
	case cmd.Inventory != nil && digest == "":
		digest = Digest(inventory.Digest(cmd.Inventory))
	case cmd.Inventory == nil && digest != "" && digest != s.knownInventory(auth):
		res.InventoryRequested = true
	}

	beat := Heartbeat{
		Satellite:            auth.Satellite,
		SeenAt:               now,
		Interval:             interval,
		StateDigest:          cmd.StateDigest,
		ConfigDigest:         cmd.ConfigDigest,
		AppliedStates:        cmd.AppliedStates,
		LastReplicationError: cmd.LastReplicationError,
		InventoryDigest:      digest,
		Inventory:            cmd.Inventory,
		Sample:               cmd.Sample,
	}
	beat.Sample.Satellite = auth.Satellite
	if s.rotations != nil {
		if pending, ok := s.rotations.Pending(auth.Satellite); ok {
			switch {
			case cmd.CredentialsGeneration == pending.Generation:
				beat.CredentialsGeneration = pending.Generation
			case cmd.CredentialsGeneration < pending.Generation && auth.Method != AuthRobot:
				res.Credentials = &pending
			}
		}
	}
	if s.sink != nil {
		s.sink.Record(beat)
	}

	if cmd.Journal != nil {
		// Ingest answers a malformed upload in the ack. Its errors get no
		// journal object rather than an ack error: the satellite drops a
		// batch after three rejections, and a database outage must not
		// cost it records (ADR-0015).
		if ack, err := s.journal.Ingest(ctx, auth.Satellite, auth.SatelliteName, *cmd.Journal); err != nil {
			res.JournalErr = err
		} else {
			res.Journal = &ack
		}
	}

	if err := s.session(ctx, auth, &res); err != nil {
		return HeartbeatResult{}, err
	}
	return res, nil
}

// session renews a session beat's token in the last third of its TTL and
// gives a robot Basic satellite without a session its first one.
func (s *HeartbeatService) session(ctx context.Context, auth SatelliteAuth, res *HeartbeatResult) error {
	if s.c.sessions == nil {
		return nil
	}
	var (
		grant SessionGrant
		ok    bool
		err   error
	)
	switch {
	case auth.Method == AuthSession:
		grant, ok, err = s.c.sessions.Renew(ctx, auth)
	case auth.Method == AuthRobot && !auth.HasSession:
		grant, ok, err = s.c.sessions.IssueIfNone(ctx, auth.Satellite)
	default:
		return nil
	}
	if err != nil {
		return fmt.Errorf("session of satellite %s: %w", auth.SatelliteName, err)
	}
	if ok {
		res.Session = &grant
	}
	return nil
}

func (s *HeartbeatService) knownInventory(auth SatelliteAuth) Digest {
	if s.sink != nil {
		if d, ok := s.sink.InventoryDigest(auth.Satellite); ok {
			return d
		}
	}
	return auth.InventoryDigest
}

// refreshEvents is the rule the sync handler always had: refresh when the
// robot expires before the second beat from now.
func refreshEvents(expires time.Time, every time.Duration, now time.Time) []string {
	if expires.IsZero() || every <= 0 {
		return nil
	}
	if expires.Before(now.Add(2 * every)) {
		return []string{"refresh_credentials"}
	}
	return nil
}

const (
	maxShortField = 255
	maxReference  = 512
	maxCPUPercent = 999.99
)

func validateBeat(cmd HeartbeatCmd) error {
	bad := func(format string, args ...any) error {
		return fmt.Errorf("%w: %s", ErrInvalid, fmt.Sprintf(format, args...))
	}
	sample := cmd.Sample
	switch {
	case len(sample.Activity) > maxShortField:
		return bad("activity longer than %d bytes", maxShortField)
	case len(cmd.StateDigest) > maxShortField, len(sample.LatestStateDigest) > maxShortField:
		return bad("state digest longer than %d bytes", maxShortField)
	case len(cmd.ConfigDigest) > maxShortField, len(sample.LatestConfigDigest) > maxShortField:
		return bad("config digest longer than %d bytes", maxShortField)
	case len(cmd.InventoryDigest) > maxShortField:
		return bad("inventory digest longer than %d bytes", maxShortField)
	case len(cmd.LastReplicationError) > 4096:
		return bad("replication error longer than 4096 bytes")
	case math.IsNaN(sample.CPUPercent) || sample.CPUPercent < 0 || sample.CPUPercent > maxCPUPercent:
		return bad("cpu percent %v outside [0, %v]", sample.CPUPercent, maxCPUPercent)
	case sample.MemoryUsedBytes < 0, sample.StorageUsedBytes < 0, sample.LastSyncDuration < 0, sample.ImageCount < 0:
		return bad("negative count")
	}
	for _, a := range cmd.AppliedStates {
		if len(a.Ref) > maxReference || len(a.Digest) > maxShortField {
			return bad("applied state %q too long", a.Ref)
		}
	}
	for _, it := range cmd.Inventory {
		if len(it.Reference) > maxReference || it.Reference == "" {
			return bad("cached image reference empty or longer than %d bytes", maxReference)
		}
		if it.SizeBytes < 0 {
			return bad("negative size of %s", it.Reference)
		}
	}
	return nil
}

// normalizeInterval returns "@every HHhMMmSSs" and its duration; empty
// stays empty.
func normalizeInterval(interval string) (string, time.Duration, error) {
	if interval == "" {
		return "", 0, nil
	}
	const prefix = "@every "
	raw, ok := strings.CutPrefix(interval, prefix)
	if !ok {
		return "", 0, fmt.Errorf("%w: heartbeat interval %q must start with %q", ErrInvalid, interval, prefix)
	}
	d, err := time.ParseDuration(raw)
	if err != nil {
		return "", 0, fmt.Errorf("%w: heartbeat interval %q: %w", ErrInvalid, interval, err)
	}
	if d < time.Second || d%time.Second != 0 {
		return "", 0, fmt.Errorf("%w: heartbeat interval %q must be whole seconds, at least one", ErrInvalid, interval)
	}
	h, m, sec := int(d/time.Hour), int(d/time.Minute)%60, int(d/time.Second)%60
	return fmt.Sprintf("@every %02dh%02dm%02ds", h, m, sec), d, nil
}
