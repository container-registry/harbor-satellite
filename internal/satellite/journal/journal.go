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
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"sync"
	"time"

	"github.com/container-registry/harbor-satellite/internal/shared/edgejournal"
	"github.com/container-registry/harbor-satellite/pkg/config"
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

type appendResult struct {
	lastSeq int64
	err     error
}

type pendingAppend struct {
	records []Record
	// done is nil for TryAppend, whose records hold queue slots instead.
	done chan appendResult
}

// Journal is safe for concurrent use.
type Journal struct {
	opts Options

	// mu guards the queue and the cursor fields below.
	mu       sync.Mutex
	queue    []pendingAppend
	queued   int
	dropped  int64
	closed   bool
	epoch    int64
	acked    int64
	floor    int64
	lastSeq  int64
	kick     chan struct{}
	stop     chan struct{}
	finished chan struct{}

	// files guards the segment list and the open segment.
	files    sync.Mutex
	segments []*segment
	active   *os.File
}

// Open opens the journal in opts.Dir, creating it and drawing an epoch when
// the directory is new. A record torn by a crash during a write is cut off
// the tail of the last segment.
func Open(opts Options) (*Journal, error) {
	if opts.Dir == "" {
		return nil, errors.New("journal directory is required")
	}
	if opts.MaxSegmentBytes <= 0 {
		opts.MaxSegmentBytes = config.DefaultJournalSegmentBytes
	}
	if opts.MaxTotalBytes <= 0 {
		opts.MaxTotalBytes = config.DefaultJournalTotalBytes
	}
	if opts.QueueSize <= 0 {
		opts.QueueSize = DefaultQueueSize
	}
	if err := os.MkdirAll(opts.Dir, 0o700); err != nil {
		return nil, fmt.Errorf("create journal directory: %w", err)
	}
	j := &Journal{
		opts:     opts,
		kick:     make(chan struct{}, 1),
		stop:     make(chan struct{}),
		finished: make(chan struct{}),
	}
	if err := j.load(); err != nil {
		return nil, err
	}
	go j.writer()
	return j, nil
}

func (j *Journal) load() error {
	epoch, ok, err := readEpoch(j.opts.Dir)
	if err != nil {
		return err
	}
	if !ok {
		epoch = time.Now().UnixMilli()
		if err = writeEpoch(j.opts.Dir, epoch); err != nil {
			return err
		}
	}
	j.epoch = epoch
	ack, err := readAck(j.opts.Dir)
	if err != nil {
		return err
	}
	if ack.Epoch == epoch {
		j.acked = ack.Acked
	}
	j.floor = ack.Floor
	segs, err := loadSegments(j.opts.Dir)
	if err != nil {
		return err
	}
	j.segments = segs
	j.lastSeq = max(j.floor, ack.LastSeq)
	if n := len(segs); n > 0 {
		j.lastSeq = max(j.lastSeq, segs[n-1].last)
	}
	return nil
}

// Epoch identifies this journal and orders journals in time: it is drawn as
// the time in Unix milliseconds and kept across restarts. Ack moves it
// forward: to Ground Control's epoch plus 1 when that one is newer, and to
// a newly drawn one (above the current) when Ground Control acknowledges a
// seq above the last persisted one, which means the directory was restored
// from a snapshot. The records and the floor stay.
func (j *Journal) Epoch() int64 {
	j.mu.Lock()
	defer j.mu.Unlock()
	return j.epoch
}

// Append assigns the next seqs to records and returns the last one once
// they are synced to disk. Concurrent calls share one sync.
func (j *Journal) Append(ctx context.Context, records ...Record) (lastSeq int64, err error) {
	if len(records) == 0 {
		j.mu.Lock()
		defer j.mu.Unlock()
		if j.closed {
			return 0, ErrClosed
		}
		return j.lastSeq, nil
	}
	done := make(chan appendResult, 1)
	j.mu.Lock()
	if j.closed {
		j.mu.Unlock()
		return 0, ErrClosed
	}
	j.queue = append(j.queue, pendingAppend{records: records, done: done})
	j.mu.Unlock()
	j.signal()
	select {
	case res := <-done:
		return res.lastSeq, res.err
	case <-ctx.Done():
		return 0, ctx.Err()
	}
}

// TryAppend never blocks: it queues records for the writer and returns
// true, or, when QueueSize records already wait for a sync, drops them and
// returns false. Dropped records get no seq: once a slot is free the
// writer appends one edgejournal.RecordDropped counting them, so seqs stay
// contiguous and the loss reaches Ground Control. The pull-through
// producer uses it.
func (j *Journal) TryAppend(records ...Record) bool {
	if len(records) == 0 {
		return true
	}
	j.mu.Lock()
	if j.closed {
		j.mu.Unlock()
		return false
	}
	if j.queued+len(records) > j.opts.QueueSize {
		j.dropped += int64(len(records))
		j.mu.Unlock()
		return false
	}
	j.queued += len(records)
	j.queue = append(j.queue, pendingAppend{records: records})
	j.mu.Unlock()
	j.signal()
	return true
}

func (j *Journal) signal() {
	select {
	case j.kick <- struct{}{}:
	default:
	}
}

// ReadFrom returns up to limit records with a seq above afterSeq, in seq
// order.
func (j *Journal) ReadFrom(afterSeq int64, limit int) ([]Record, error) {
	if limit <= 0 {
		return nil, nil
	}
	j.mu.Lock()
	if j.closed {
		j.mu.Unlock()
		return nil, ErrClosed
	}
	afterSeq = max(afterSeq, j.floor)
	j.mu.Unlock()

	j.files.Lock()
	defer j.files.Unlock()
	var out []Record
	for _, seg := range j.segments {
		if seg.last <= afterSeq {
			continue
		}
		records, _, err := readSegment(seg.path)
		if err != nil {
			return nil, err
		}
		for _, r := range records {
			if r.Seq <= afterSeq {
				continue
			}
			out = append(out, r)
			if len(out) == limit {
				return out, nil
			}
		}
	}
	return out, nil
}

// Floor is the seq up to which the journal holds nothing for Ground
// Control: its ack, raised past records it gave up (the size cap, Drop).
// It never decreases, also not when the epoch moves. Uploads send it as
// after_seq.
func (j *Journal) Floor() int64 {
	j.mu.Lock()
	defer j.mu.Unlock()
	return j.floor
}

// Acked is the highest seq of the current epoch Ground Control
// acknowledged.
func (j *Journal) Acked() int64 {
	j.mu.Lock()
	defer j.mu.Unlock()
	return j.acked
}

// LastSeq is the seq of the last record synced to disk.
func (j *Journal) LastSeq() int64 {
	j.mu.Lock()
	defer j.mu.Unlock()
	return j.lastSeq
}

// Ack applies Ground Control's answer to an upload. With this journal's
// epoch it records the ack and deletes the segments that hold nothing
// newer; an older ack does nothing. A newer epoch, or an ack above the last
// persisted seq, moves the epoch as Epoch describes.
func (j *Journal) Ack(epoch, ackedSeq int64) error {
	j.mu.Lock()
	defer j.mu.Unlock()
	if j.closed {
		return ErrClosed
	}
	switch {
	case epoch < j.epoch:
		return nil
	case epoch > j.epoch:
		return j.moveEpochLocked(epoch + 1)
	case ackedSeq > j.lastSeq:
		return j.moveEpochLocked(max(time.Now().UnixMilli(), j.epoch+1))
	case ackedSeq <= j.acked:
		return nil
	}
	j.acked = ackedSeq
	j.floor = max(j.floor, ackedSeq)
	if err := j.persistAckLocked(); err != nil {
		return err
	}
	return j.deleteBelowFloorLocked()
}

func (j *Journal) moveEpochLocked(epoch int64) error {
	if err := writeEpoch(j.opts.Dir, epoch); err != nil {
		return err
	}
	j.epoch = epoch
	j.acked = 0
	return j.persistAckLocked()
}

// Drop gives up the records up to through without an ack, so the floor
// rises past them and Ground Control records a gap. The status report uses
// it for a batch Ground Control rejected three times in a row.
func (j *Journal) Drop(through int64) error {
	j.mu.Lock()
	defer j.mu.Unlock()
	if j.closed {
		return ErrClosed
	}
	through = min(through, j.lastSeq)
	if through <= j.floor {
		return nil
	}
	j.floor = through
	if err := j.persistAckLocked(); err != nil {
		return err
	}
	return j.deleteBelowFloorLocked()
}

func (j *Journal) Close() error {
	j.mu.Lock()
	if j.closed {
		j.mu.Unlock()
		return ErrClosed
	}
	j.closed = true
	j.mu.Unlock()
	close(j.stop)
	<-j.finished

	j.files.Lock()
	defer j.files.Unlock()
	if j.active == nil {
		return nil
	}
	err := j.active.Close()
	j.active = nil
	return err
}

func (j *Journal) persistAckLocked() error {
	return writeAck(j.opts.Dir, ackFile{Epoch: j.epoch, Acked: j.acked, Floor: j.floor, LastSeq: j.lastSeq})
}

// deleteBelowFloorLocked removes the segments whose records all lie at or
// below the floor, except the one being written. j.mu is held.
func (j *Journal) deleteBelowFloorLocked() error {
	j.files.Lock()
	defer j.files.Unlock()
	keep := j.segments[:0]
	var removed bool
	for i, seg := range j.segments {
		if i < len(j.segments)-1 && seg.last <= j.floor {
			if err := os.Remove(seg.path); err != nil && !errors.Is(err, os.ErrNotExist) {
				return fmt.Errorf("delete journal segment: %w", err)
			}
			removed = true
			continue
		}
		keep = append(keep, seg)
	}
	j.segments = keep
	if removed {
		return syncDir(j.opts.Dir)
	}
	return nil
}

func (j *Journal) writer() {
	defer close(j.finished)
	for {
		select {
		case <-j.kick:
		case <-j.stop:
			j.flush()
			return
		}
		if j.opts.SyncInterval > 0 {
			timer := time.NewTimer(j.opts.SyncInterval)
			select {
			case <-timer.C:
			case <-j.stop:
				timer.Stop()
			}
		}
		j.flush()
	}
}

// flush writes and syncs everything queued, then queues the drop count.
func (j *Journal) flush() {
	j.mu.Lock()
	batch := j.queue
	j.queue = nil
	j.mu.Unlock()
	if len(batch) == 0 {
		return
	}
	results, err := j.write(batch)

	j.mu.Lock()
	for i, p := range batch {
		if p.done == nil {
			j.queued -= len(p.records)
			continue
		}
		p.done <- appendResult{lastSeq: results[i], err: err}
	}
	dropped := j.dropped
	j.dropped = 0
	if dropped > 0 && !j.closed {
		payload, merr := json.Marshal(edgejournal.Dropped{SchemaVersion: edgejournal.SchemaVersion, Count: dropped})
		if merr == nil {
			j.queue = append(j.queue, pendingAppend{records: []Record{{
				Type: edgejournal.RecordDropped, RecordedAt: time.Now().UTC(), Payload: payload,
			}}})
		}
	}
	more := len(j.queue) > 0
	j.mu.Unlock()
	if more {
		j.signal()
	}
}

// write appends the batch and syncs it once; it returns the last seq of
// each pending append.
func (j *Journal) write(batch []pendingAppend) ([]int64, error) {
	j.mu.Lock()
	next := j.lastSeq + 1
	j.mu.Unlock()

	j.files.Lock()
	last := next - 1
	results := make([]int64, len(batch))
	var err error
	for i, p := range batch {
		for _, r := range p.records {
			r.Seq = last + 1
			if err = j.writeRecord(r); err != nil {
				break
			}
			last = r.Seq
		}
		results[i] = last
		if err != nil {
			break
		}
	}
	if err == nil && j.active != nil {
		err = j.active.Sync()
	}
	if err != nil {
		err = errors.Join(err, j.cutTornTail())
	}
	j.files.Unlock()

	j.mu.Lock()
	defer j.mu.Unlock()
	// Records written before a failure stay on disk, so their seqs are
	// taken even when the batch fails.
	j.lastSeq = last
	if err != nil {
		return results, err
	}
	return results, j.enforceCapLocked()
}

// cutTornTail truncates the active segment to its last complete frame after
// a failed write, so later frames are not hidden behind a partial one.
// j.files is held.
func (j *Journal) cutTornTail() error {
	if j.active == nil || len(j.segments) == 0 {
		return nil
	}
	if err := j.active.Truncate(j.segments[len(j.segments)-1].size); err != nil {
		return fmt.Errorf("truncate torn journal frame: %w", err)
	}
	return nil
}

// writeRecord bounds the payload's strings (edgejournal.BoundPayload), so
// no record can push a heartbeat past Ground Control's body limit.
func (j *Journal) writeRecord(r Record) error {
	r.Payload = edgejournal.BoundPayload(r.Payload)
	frame, err := encodeRecord(r)
	if err != nil {
		return err
	}
	n := len(j.segments)
	if n == 0 || j.segments[n-1].size >= j.opts.MaxSegmentBytes || j.active == nil {
		if err := j.openSegment(r.Seq, n == 0 || j.segments[n-1].size >= j.opts.MaxSegmentBytes); err != nil {
			return err
		}
		n = len(j.segments)
	}
	seg := j.segments[n-1]
	if _, err := j.active.Write(frame); err != nil {
		return fmt.Errorf("write journal record: %w", err)
	}
	seg.size += int64(len(frame))
	seg.last = r.Seq
	return nil
}

// openSegment opens the segment to append to: a new one starting at seq,
// or, after a restart, the last one on disk.
func (j *Journal) openSegment(seq int64, fresh bool) error {
	if j.active != nil {
		if err := j.active.Sync(); err != nil {
			return fmt.Errorf("sync journal segment: %w", err)
		}
		if err := j.active.Close(); err != nil {
			return fmt.Errorf("close journal segment: %w", err)
		}
		j.active = nil
	}
	if !fresh {
		seg := j.segments[len(j.segments)-1]
		f, err := os.OpenFile(seg.path, os.O_WRONLY|os.O_APPEND, 0o600)
		if err != nil {
			return fmt.Errorf("open journal segment: %w", err)
		}
		j.active = f
		return nil
	}
	path := segmentPath(j.opts.Dir, seq)
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_APPEND, 0o600)
	if err != nil {
		return fmt.Errorf("create journal segment: %w", err)
	}
	if err := syncDir(j.opts.Dir); err != nil {
		_ = f.Close()
		return err
	}
	j.active = f
	j.segments = append(j.segments, &segment{path: path, first: seq, last: seq - 1})
	return nil
}

// enforceCapLocked drops the oldest segments while the journal is over
// MaxTotalBytes, raising the floor past them. j.mu is held.
func (j *Journal) enforceCapLocked() error {
	j.files.Lock()
	var total int64
	for _, seg := range j.segments {
		total += seg.size
	}
	floor := j.floor
	for len(j.segments) > 1 && total > j.opts.MaxTotalBytes {
		seg := j.segments[0]
		if err := os.Remove(seg.path); err != nil && !errors.Is(err, os.ErrNotExist) {
			j.files.Unlock()
			return fmt.Errorf("delete journal segment: %w", err)
		}
		total -= seg.size
		floor = max(floor, seg.last)
		j.segments = j.segments[1:]
	}
	j.files.Unlock()
	if floor == j.floor {
		return nil
	}
	j.floor = floor
	if err := syncDir(j.opts.Dir); err != nil {
		return err
	}
	return j.persistAckLocked()
}
