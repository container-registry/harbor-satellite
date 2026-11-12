package fleet

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/container-registry/harbor-satellite/internal/shared/edgejournal"
)

// MaxJournalRecords bounds one upload, as the sync contract does.
const MaxJournalRecords = 1000

// Edge events re-emit accepted journal records into the outbox (plan item
// 9.4). Nothing handles them, so they are log-only: appended already done
// for readers such as webhooks.
const (
	EventEdgeReplicated EventKind = "EdgeReplicated"
	EventEdgeDeleted    EventKind = "EdgeDeleted"
	// EventEdgeArtifactFailed is a replication or a deletion that failed.
	EventEdgeArtifactFailed EventKind = "EdgeArtifactFailed"
	EventEdgePullThrough    EventKind = "EdgePullThrough"
	EventEdgeCRIConfigured  EventKind = "EdgeCRIConfigured"
	// EventEdgeJournalGap reports records Ground Control will never have
	// events for; its payload is an EdgeJournalGapPayload.
	EventEdgeJournalGap EventKind = "EdgeJournalGap"
)

// Causes of an EventEdgeJournalGap.
const (
	// GapNewEpoch: a new or reset cursor, and the satellite's floor above
	// zero.
	GapNewEpoch = "new_epoch"
	// GapCursorBehind: the satellite's floor is above the cursor, after it
	// dropped records at its size cap or after a batch Ground Control kept
	// rejecting, or because Ground Control lost its cursor (a restored
	// database, a re-registered satellite).
	GapCursorBehind = "cursor_behind"
	// GapDropped: the satellite dropped records under backpressure and
	// counted them in an edgejournal.RecordDropped.
	GapDropped = "dropped"
	// GapRejectedPrefix starts the cause of a record Ground Control could
	// not map; the reason follows.
	GapRejectedPrefix = "rejected: "
)

// JournalUpload is the journal part of a heartbeat (plan item 6.5,
// ADR-0015).
type JournalUpload struct {
	// Epoch identifies the satellite's journal; epochs are time-ordered.
	Epoch int64
	// AfterSeq is the satellite's floor: it holds nothing at or below it
	// for Ground Control any more. Records start right above it.
	AfterSeq int64
	Records  []JournalRecord
}

// JournalRecord is one uploaded record; Payload is the edgejournal payload
// of its Type.
type JournalRecord struct {
	Seq        int64
	Type       edgejournal.RecordType
	RecordedAt time.Time
	Payload    json.RawMessage
}

// JournalAck answers an upload. Epoch is Ground Control's epoch for the
// satellite and AckedSeq the seq it holds that epoch up to; a satellite
// whose epoch is older continues as Epoch+1. Error says why an upload was
// rejected, which leaves the cursor as it was.
type JournalAck struct {
	Epoch, AckedSeq int64
	Error           string
}

// JournalCursor is how far Ground Control holds a satellite's journal.
// Fresh is set when opening it created the cursor or moved it to a newer
// epoch.
type JournalCursor struct {
	Epoch, AckedSeq int64
	Fresh           bool
}

// EdgeEvent is a log-only edge event about a satellite; its aggregate is
// the satellite's edge (AggregateEdge, the satellite's ID).
type EdgeEvent struct {
	Kind    EventKind
	Payload any
}

// EdgeOrigin is where an edge event comes from. An edge event's payload is
// the record's edgejournal payload with these fields added at the same
// level, so its data matches ADR-0011's.
type EdgeOrigin struct {
	Name       string    `json:"name"`
	Epoch      int64     `json:"epoch"`
	Seq        int64     `json:"seq"`
	RecordedAt time.Time `json:"recorded_at"`
}

// EdgeJournalGapPayload is the payload of EventEdgeJournalGap: Ground
// Control has no events for the seqs From to Through, for Cause.
type EdgeJournalGapPayload struct {
	Name    string `json:"name"`
	Epoch   int64  `json:"epoch"`
	From    int64  `json:"from"`
	Through int64  `json:"through"`
	Cause   string `json:"cause"`
	// Dropped is how many records the satellite dropped (cause GapDropped).
	Dropped int64 `json:"dropped,omitempty"`
}

// NewEdgeEvent maps an accepted record to its edge event.
//   - replication is EventEdgeReplicated or EventEdgeDeleted by its action,
//     and EventEdgeArtifactFailed when it carries an error;
//   - cri is EventEdgeCRIConfigured and pull_through EventEdgePullThrough;
//   - dropped is an EventEdgeJournalGap at the record's seq with cause
//     GapDropped and the count.
//
// Payload fields Ground Control does not know pass through, because
// schema versions only add fields. An unknown type or action, or a payload
// that is not its type's JSON object, is ErrInvalid.
func NewEdgeEvent(name string, epoch int64, record JournalRecord) (EdgeEvent, error) {
	event, reason := mapRecord(name, epoch, record)
	if reason != "" {
		return EdgeEvent{}, fmt.Errorf("%w: journal record %d: %s", ErrInvalid, record.Seq, reason)
	}
	return event, nil
}

// mapRecord is NewEdgeEvent with the reason it rejects a record instead of
// an error, so Ingest can put the reason into a gap's cause.
func mapRecord(name string, epoch int64, record JournalRecord) (EdgeEvent, string) {
	// PostgreSQL's jsonb refuses \u0000, and an outbox insert that fails
	// would fail the whole upload again on every retry.
	if bytes.Contains(record.Payload, []byte(`\u0000`)) {
		return EdgeEvent{}, "payload holds a NUL character"
	}
	var fields map[string]any
	if err := json.Unmarshal(record.Payload, &fields); err != nil || fields == nil {
		return EdgeEvent{}, "payload is not a JSON object"
	}
	var kind EventKind
	switch record.Type {
	case edgejournal.RecordReplication:
		var r edgejournal.Replication
		if err := json.Unmarshal(record.Payload, &r); err != nil {
			return EdgeEvent{}, fmt.Sprintf("replication payload: %v", err)
		}
		switch {
		case r.Action != edgejournal.ActionReplicated && r.Action != edgejournal.ActionDeleted:
			return EdgeEvent{}, fmt.Sprintf("replication action %q", r.Action)
		case r.Error != "":
			kind = EventEdgeArtifactFailed
		case r.Action == edgejournal.ActionDeleted:
			kind = EventEdgeDeleted
		default:
			kind = EventEdgeReplicated
		}
	case edgejournal.RecordCRI:
		var c edgejournal.CRI
		if err := json.Unmarshal(record.Payload, &c); err != nil {
			return EdgeEvent{}, fmt.Sprintf("cri payload: %v", err)
		}
		kind = EventEdgeCRIConfigured
	case edgejournal.RecordPullThrough:
		var p edgejournal.PullThrough
		if err := json.Unmarshal(record.Payload, &p); err != nil {
			return EdgeEvent{}, fmt.Sprintf("pull_through payload: %v", err)
		}
		kind = EventEdgePullThrough
	case edgejournal.RecordDropped:
		var d edgejournal.Dropped
		if err := json.Unmarshal(record.Payload, &d); err != nil || d.Count < 0 {
			return EdgeEvent{}, "dropped payload"
		}
		return EdgeEvent{Kind: EventEdgeJournalGap, Payload: EdgeJournalGapPayload{
			Name: name, Epoch: epoch, From: record.Seq, Through: record.Seq, Cause: GapDropped, Dropped: d.Count,
		}}, ""
	default:
		return EdgeEvent{}, fmt.Sprintf("unknown record type %q", record.Type)
	}
	// Unknown fields pass through: schema versions only add fields.
	fields["name"], fields["epoch"], fields["seq"], fields["recorded_at"] = name, epoch, record.Seq, record.RecordedAt
	return EdgeEvent{Kind: kind, Payload: fields}, ""
}

// EdgeKinds are the kinds of edge events, the only ones JournalRepo.Advance
// appends.
func EdgeKinds() []EventKind {
	return []EventKind{
		EventEdgeReplicated, EventEdgeDeleted, EventEdgeArtifactFailed,
		EventEdgePullThrough, EventEdgeCRIConfigured, EventEdgeJournalGap,
	}
}

// JournalRepo keeps one journal cursor per satellite (plan item 6.5).
// Accepted records are stored only as outbox events.
type JournalRepo interface {
	// OpenCursor returns the satellite's cursor after creating it or, when
	// epoch is newer than the stored one, moving it to epoch with AckedSeq
	// 0; Fresh reports either. An older epoch leaves it, so the returned
	// Epoch tells a delayed upload of a replaced journal apart. It locks
	// the cursor until the transaction ends.
	OpenCursor(ctx context.Context, satellite SatelliteID, epoch int64) (JournalCursor, error)
	// Advance moves the cursor of epoch forward to ackedSeq and appends
	// events, log-only and in order, in one statement. It takes edge
	// events only: any other kind is ErrInvalid, so it never becomes a
	// second way into the outbox. ErrConflict when the cursor is not at
	// epoch or not below ackedSeq; then nothing is appended.
	Advance(ctx context.Context, satellite SatelliteID, epoch, ackedSeq int64, events []EdgeEvent) error
}

// JournalService ingests satellite journals (plan items 6.5 and 9.4,
// ADR-0015). It is a dependency of the heartbeat service (plan item 4),
// whose Beat records the heartbeat first and then calls Ingest, so a
// journal never makes a satellite stale. The emission must not ship
// before the outbox retention of plan item 9.1, or edge events grow the
// outbox without bound.
type JournalService struct {
	repo Repository
}

// NewJournalService runs on repo, the fleet plane's repository; a nil repo
// is ErrInvalid.
func NewJournalService(repo Repository) (*JournalService, error) {
	if repo == nil {
		return nil, fmt.Errorf("%w: the journal service needs a repository", ErrInvalid)
	}
	return &JournalService{repo: repo}, nil
}

// Ingest accepts one upload of satellite name and answers it.
//
// Only a malformed envelope is rejected: an Epoch below 1, an AfterSeq
// below 0, more than MaxJournalRecords records, a seq below 1, or records
// that do not run on from AfterSeq+1 one by one. It touches nothing and is
// answered {upload.Epoch, upload.AfterSeq, reason}, which is not an error
// of the heartbeat.
//
// Otherwise one transaction opens the cursor. An upload of an older epoch
// than the cursor's is answered {cursor.Epoch, 0}. Within the epoch the
// base is the greater of the cursor's AckedSeq and AfterSeq, and the ack
// the greater of the base and the last seq. One Advance appends, in seq
// order:
//   - an EventEdgeJournalGap for a base above the cursor, with cause
//     GapNewEpoch for a fresh cursor and GapCursorBehind otherwise;
//   - for every record above the base, its NewEdgeEvent, or for a record
//     NewEdgeEvent rejects an EventEdgeJournalGap for its seq with cause
//     GapRejectedPrefix plus the reason; the record is still acked.
//
// Records at or below the cursor are duplicates and emit nothing; with
// nothing to append there is no Advance. A journaled heartbeat therefore
// costs at most four statements: BEGIN, OpenCursor, Advance, COMMIT, plus
// a second OpenJournalCursor when it raced a concurrent first upload. A
// failure of the repository is returned as an error; the heartbeat then
// answers without a journal object.
func (s *JournalService) Ingest(ctx context.Context, satellite SatelliteID, name string, upload JournalUpload) (JournalAck, error) {
	if reason := envelopeProblem(upload); reason != "" {
		return JournalAck{Epoch: upload.Epoch, AckedSeq: upload.AfterSeq, Error: reason}, nil
	}
	var ack JournalAck
	err := s.repo.WithTx(ctx, func(tx Tx) error {
		cursor, err := tx.Journal().OpenCursor(ctx, satellite, upload.Epoch)
		if err != nil {
			return fmt.Errorf("open journal cursor: %w", err)
		}
		if cursor.Epoch > upload.Epoch {
			ack = JournalAck{Epoch: cursor.Epoch}
			return nil
		}
		base := max(cursor.AckedSeq, upload.AfterSeq)
		acked := base
		if n := len(upload.Records); n > 0 {
			acked = max(base, upload.Records[n-1].Seq)
		}
		ack = JournalAck{Epoch: cursor.Epoch, AckedSeq: acked}
		if acked == cursor.AckedSeq {
			return nil
		}
		events := make([]EdgeEvent, 0, len(upload.Records)+1)
		if base > cursor.AckedSeq {
			cause := GapCursorBehind
			if cursor.Fresh {
				cause = GapNewEpoch
			}
			events = append(events, gapEvent(name, cursor.Epoch, cursor.AckedSeq+1, base, cause))
		}
		for _, record := range upload.Records {
			if record.Seq <= base {
				continue
			}
			event, reason := mapRecord(name, cursor.Epoch, record)
			if reason != "" {
				event = gapEvent(name, cursor.Epoch, record.Seq, record.Seq, GapRejectedPrefix+reason)
			}
			events = append(events, event)
		}
		if err := tx.Journal().Advance(ctx, satellite, cursor.Epoch, acked, events); err != nil {
			return fmt.Errorf("advance journal cursor: %w", err)
		}
		return nil
	})
	if err != nil {
		return JournalAck{}, fmt.Errorf("journal of satellite %s: %w", name, err)
	}
	return ack, nil
}

func gapEvent(name string, epoch, from, through int64, cause string) EdgeEvent {
	return EdgeEvent{Kind: EventEdgeJournalGap, Payload: EdgeJournalGapPayload{
		Name: name, Epoch: epoch, From: from, Through: through, Cause: cause,
	}}
}

// envelopeProblem says what makes an upload malformed, or "".
func envelopeProblem(upload JournalUpload) string {
	switch {
	case upload.Epoch < 1:
		return fmt.Sprintf("journal epoch %d is below 1", upload.Epoch)
	case upload.AfterSeq < 0:
		return fmt.Sprintf("journal after_seq %d is negative", upload.AfterSeq)
	case len(upload.Records) > MaxJournalRecords:
		return fmt.Sprintf("journal upload has %d records, at most %d", len(upload.Records), MaxJournalRecords)
	}
	for i, record := range upload.Records {
		if want := upload.AfterSeq + int64(i) + 1; record.Seq != want {
			return fmt.Sprintf("journal record %d has seq %d, want %d: records run on from after_seq one by one", i, record.Seq, want)
		}
	}
	return ""
}
