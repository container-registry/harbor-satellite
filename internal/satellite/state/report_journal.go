package state

import (
	"errors"
	"sync"

	"github.com/container-registry/harbor-satellite/internal/satellite/gcclient"
	"github.com/container-registry/harbor-satellite/internal/satellite/journal"
	"github.com/container-registry/harbor-satellite/internal/satellite/telemetry"
	"github.com/container-registry/harbor-satellite/pkg/config"
	"github.com/container-registry/harbor-satellite/pkg/groundcontrol"
)

// maxRejections is how often Ground Control may reject the same batch
// before the satellite drops it (ADR-0015).
const maxRejections = 3

// growAfter is how many beats in a row must pass at a batch cap that a 413
// lowered before the cap doubles again.
const growAfter = 4

// SetJournal makes the report carry a journal upload (plan item 6.5): the
// journal's epoch, its floor as after_seq and the records above it, at
// most min(app_config.journal.upload_batch, config.MaxJournalUploadBatch),
// or one until Ground Control has answered a journal once. The report applies the answer with
// journal.Ack; after three answers in a row that reject the same batch it
// drops the batch, so Ground Control records a gap instead of the
// satellite resending it forever. A report with nothing above the last ack
// and no raised floor carries no journal.
func (s *StatusReportingProcess) SetJournal(j *journal.Journal) {
	s.upload.set(j)
}

// SetRunSummarySource makes the report carry the last run's digests,
// applied_states, duration and last_replication_error (plan item 6.6).
func (s *StatusReportingProcess) SetRunSummarySource(src RunSummarySource) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.runSummary = src
}

// journalUploader is the satellite side of the upload protocol. A heartbeat
// runs at a time, so its fields change only between next and answer.
type journalUploader struct {
	mu sync.Mutex
	j  *journal.Journal
	// answered is set once Ground Control answered a journal; until then
	// a heartbeat carries one record.
	answered bool
	// reportedFloor is the floor Ground Control last accepted, so a floor
	// the journal raised since is sent even without records.
	reportedFloor    int64
	rejected         int
	rejectedFirst    int64
	rejectedLast     int64
	rejectedAfterSeq int64
	// oversize is the reaction to Ground Control's 413 (SYNC_MAX_BODY_BYTES).
	oversize oversizeState
}

// oversizeState shrinks heartbeats that Ground Control refused as too
// large. A 413 halves the journal batch (floor 1); a 413 at one record, or
// without records, makes the next beat bare: no journal and no cached-image
// inventory, so last_seen keeps moving. After a bare beat passes the next
// one tries one record again. The cap doubles after growAfter passing beats
// and is lifted once a beat uploads fewer records than the cap, since the
// backlog that held the large records has then drained.
type oversizeState struct {
	limit  int // journal batch cap; 0 is none
	bare   bool
	passed int // beats in a row that passed at limit
}

func (u *journalUploader) set(j *journal.Journal) {
	u.mu.Lock()
	defer u.mu.Unlock()
	u.j = j
	u.answered = false
	u.reportedFloor = 0
	u.rejected = 0
	if j != nil {
		u.reportedFloor = j.Acked()
	}
}

// next returns the upload for this heartbeat, or nil when there is nothing
// to send.
func (u *journalUploader) next(uploadBatch int) (*groundcontrol.JournalUpload, error) {
	u.mu.Lock()
	defer u.mu.Unlock()
	if u.j == nil {
		return nil, nil
	}
	limit := uploadBatch
	if limit <= 0 {
		limit = config.DefaultJournalUploadBatch
	}
	limit = min(limit, config.MaxJournalUploadBatch)
	if !u.answered {
		limit = 1
	}
	if u.oversize.bare {
		return nil, nil
	}
	if u.oversize.limit > 0 {
		limit = min(limit, u.oversize.limit)
	}
	floor := u.j.Floor()
	records, err := u.j.ReadFrom(floor, limit)
	if err != nil {
		return nil, err
	}
	if len(records) == 0 && floor <= u.reportedFloor {
		return nil, nil
	}
	upload := &groundcontrol.JournalUpload{Epoch: u.j.Epoch(), AfterSeq: floor, Records: make([]groundcontrol.JournalRecord, 0, len(records))}
	for _, r := range records {
		upload.Records = append(upload.Records, groundcontrol.JournalRecord{
			Seq: r.Seq, Type: string(r.Type), RecordedAt: r.RecordedAt, Payload: r.Payload,
		})
	}
	return upload, nil
}

// answer applies Ground Control's answer to upload and returns its outcome
// (telemetry.JournalAccepted, JournalRejected or JournalDropped); nil means
// it ignores journals and gives no outcome.
func (u *journalUploader) answer(upload groundcontrol.JournalUpload, ack *gcclient.JournalAck) (string, error) {
	if ack == nil {
		return "", nil
	}
	u.mu.Lock()
	defer u.mu.Unlock()
	if u.j == nil {
		return "", nil
	}
	u.answered = true
	if err := u.j.Ack(ack.Epoch, ack.AckedSeq); err != nil {
		return "", err
	}
	if ack.Error == "" {
		u.rejected = 0
		u.reportedFloor = 0
		if ack.Epoch == upload.Epoch {
			u.reportedFloor = max(upload.AfterSeq, ack.AckedSeq)
		}
		return telemetry.JournalAccepted, nil
	}
	if len(upload.Records) == 0 {
		return telemetry.JournalRejected, errors.New("journal upload rejected: " + ack.Error)
	}
	first, last := upload.Records[0].Seq, upload.Records[len(upload.Records)-1].Seq
	if u.rejected > 0 && first == u.rejectedFirst && last == u.rejectedLast && upload.AfterSeq == u.rejectedAfterSeq {
		u.rejected++
	} else {
		u.rejected, u.rejectedFirst, u.rejectedLast, u.rejectedAfterSeq = 1, first, last, upload.AfterSeq
	}
	if u.rejected < maxRejections {
		return telemetry.JournalRejected, errors.New("journal upload rejected: " + ack.Error)
	}
	u.rejected = 0
	if err := u.j.Drop(last); err != nil {
		return telemetry.JournalRejected, err
	}
	return telemetry.JournalDropped, errors.New("journal batch dropped after repeated rejection: " + ack.Error)
}

// bare reports whether this beat goes without journal and inventory.
func (u *journalUploader) bare() bool {
	u.mu.Lock()
	defer u.mu.Unlock()
	return u.oversize.bare
}

// tooLarge applies a 413 to the beat that carried upload (nil without a
// journal) and returns the telemetry.TooLarge* reaction.
func (u *journalUploader) tooLarge(upload *groundcontrol.JournalUpload) string {
	u.mu.Lock()
	defer u.mu.Unlock()
	o := &u.oversize
	o.passed = 0
	switch {
	case o.bare:
		return telemetry.TooLargeNone
	case upload != nil && len(upload.Records) > 1:
		o.limit = len(upload.Records) / 2
		return telemetry.TooLargeHalvedBatch
	default:
		o.bare = true
		return telemetry.TooLargeWithoutJournal
	}
}

// delivered applies a beat Ground Control accepted. uploadBatch is
// app_config.journal.upload_batch.
func (u *journalUploader) delivered(upload *groundcontrol.JournalUpload, uploadBatch int) {
	u.mu.Lock()
	defer u.mu.Unlock()
	o := &u.oversize
	switch {
	case o.bare:
		o.bare, o.limit, o.passed = false, 1, 0
	case o.limit == 0:
	case upload == nil || len(upload.Records) < o.limit:
		*o = oversizeState{}
	default:
		o.passed++
		if o.passed < growAfter {
			return
		}
		o.limit, o.passed = o.limit*2, 0
		if uploadBatch <= 0 {
			uploadBatch = config.DefaultJournalUploadBatch
		}
		if o.limit >= min(uploadBatch, config.MaxJournalUploadBatch) {
			*o = oversizeState{}
		}
	}
}
