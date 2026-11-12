// Package journal is the satellite's durable record of what happened at the
// edge (plan item 6.4, ADR-0015): replication results per artifact, CRI
// results and pull-through events, in the vocabulary of
// internal/shared/edgejournal. Records are appended to segment files under
// the config directory, survive a crash, and are uploaded with the heartbeat
// until Ground Control acknowledges them.
//
// Durability: a segment is length-prefixed JSON with a CRC32C per record;
// the directory is synced when a segment is created or deleted, and the
// epoch and ack files are replaced atomically (temporary file, sync,
// rename, directory sync).
package journal

import (
	"context"
	"errors"
	"time"

	"github.com/container-registry/harbor-satellite/internal/shared/notimpl"
)

// DefaultQueueSize is how many records TryAppend may queue by default.
const DefaultQueueSize = 1024

var ErrClosed = errors.New("journal closed")

// Options configure a journal. Only Dir is required.
type Options struct {
	// Dir holds the segment files (seg-<first seq>.log) and the epoch and
	// ack files. Open creates it.
	Dir string
	// MaxSegmentBytes starts a new segment once the current one is this
	// large; zero is config.DefaultJournalSegmentBytes.
	MaxSegmentBytes int64
	// MaxTotalBytes caps the segments on disk; zero is
	// config.DefaultJournalTotalBytes. Beyond it the oldest unacknowledged
	// records are dropped and the floor rises past them.
	MaxTotalBytes int64
	// SyncInterval is how long the writer gathers records before it syncs
	// them in one batch (group commit); zero syncs each batch as soon as
	// the previous sync has finished.
	SyncInterval time.Duration
	// QueueSize bounds the records accepted but not yet synced; slots are
	// released after each sync. Zero is DefaultQueueSize. TryAppend drops
	// records beyond it.
	QueueSize int
}

// Journal is safe for concurrent use.
type Journal struct{}

// Open opens the journal in opts.Dir, creating it and drawing an epoch when
// the directory is new. A record torn by a crash during a write is cut off
// the tail of the last segment.
func Open(opts Options) (*Journal, error) {
	return nil, notimpl.Err("6.4 journal open")
}

// Epoch identifies this journal and orders journals in time: it is drawn as
// the time in Unix milliseconds and kept across restarts. Ack moves it
// forward: to Ground Control's epoch plus 1 when that one is newer, and to
// a newly drawn one (above the current) when Ground Control acknowledges a
// seq above the last persisted one, which means the directory was restored
// from a snapshot. The records and the floor stay.
func (j *Journal) Epoch() int64 {
	return 0
}

// Append assigns the next seqs to records and returns the last one once
// they are synced to disk. Concurrent calls share one sync.
func (j *Journal) Append(ctx context.Context, records ...Record) (lastSeq int64, err error) {
	return 0, notimpl.Err("6.4 journal append")
}

// TryAppend never blocks: it queues records for the writer and returns
// true, or, when QueueSize records already wait for a sync, drops them and
// returns false. Dropped records get no seq: once a slot is free the
// writer appends one edgejournal.RecordDropped counting them, so seqs stay
// contiguous and the loss reaches Ground Control. The pull-through
// producer uses it.
func (j *Journal) TryAppend(records ...Record) bool {
	return false
}

// ReadFrom returns up to limit records with a seq above afterSeq, in seq
// order.
func (j *Journal) ReadFrom(afterSeq int64, limit int) ([]Record, error) {
	return nil, notimpl.Err("6.4 journal read")
}

// Floor is the seq up to which the journal holds nothing for Ground
// Control: its ack, raised past records it gave up (the size cap, Drop).
// It never decreases, also not when the epoch moves. Uploads send it as
// after_seq.
func (j *Journal) Floor() int64 {
	return 0
}

// Acked is the highest seq of the current epoch Ground Control
// acknowledged.
func (j *Journal) Acked() int64 {
	return 0
}

// Ack applies Ground Control's answer to an upload. With this journal's
// epoch it records the ack and deletes the segments that hold nothing
// newer; an older ack does nothing. A newer epoch, or an ack above the last
// persisted seq, moves the epoch as Epoch describes.
func (j *Journal) Ack(epoch, ackedSeq int64) error {
	return notimpl.Err("6.4 journal ack")
}

// Drop gives up the records up to through without an ack, so the floor
// rises past them and Ground Control records a gap. The status report uses
// it for a batch Ground Control rejected three times in a row.
func (j *Journal) Drop(through int64) error {
	return notimpl.Err("6.4 journal drop")
}

func (j *Journal) Close() error {
	return notimpl.Err("6.4 journal close")
}
