package state

import (
	"errors"
	"sync"

	"github.com/container-registry/harbor-satellite/internal/satellite/gcclient"
	"github.com/container-registry/harbor-satellite/internal/satellite/journal"
	"github.com/container-registry/harbor-satellite/pkg/config"
	"github.com/container-registry/harbor-satellite/pkg/groundcontrol"
)

// maxRejections is how often Ground Control may reject the same batch
// before the satellite drops it (ADR-0015).
const maxRejections = 3

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

// answer applies Ground Control's answer to upload; nil means it ignores
// journals.
func (u *journalUploader) answer(upload groundcontrol.JournalUpload, ack *gcclient.JournalAck) error {
	if ack == nil {
		return nil
	}
	u.mu.Lock()
	defer u.mu.Unlock()
	if u.j == nil {
		return nil
	}
	u.answered = true
	if err := u.j.Ack(ack.Epoch, ack.AckedSeq); err != nil {
		return err
	}
	if ack.Error == "" {
		u.rejected = 0
		u.reportedFloor = 0
		if ack.Epoch == upload.Epoch {
			u.reportedFloor = max(upload.AfterSeq, ack.AckedSeq)
		}
		return nil
	}
	if len(upload.Records) == 0 {
		return errors.New("journal upload rejected: " + ack.Error)
	}
	first, last := upload.Records[0].Seq, upload.Records[len(upload.Records)-1].Seq
	if u.rejected > 0 && first == u.rejectedFirst && last == u.rejectedLast && upload.AfterSeq == u.rejectedAfterSeq {
		u.rejected++
	} else {
		u.rejected, u.rejectedFirst, u.rejectedLast, u.rejectedAfterSeq = 1, first, last, upload.AfterSeq
	}
	if u.rejected < maxRejections {
		return errors.New("journal upload rejected: " + ack.Error)
	}
	u.rejected = 0
	if err := u.j.Drop(last); err != nil {
		return err
	}
	return errors.New("journal batch dropped after repeated rejection: " + ack.Error)
}
