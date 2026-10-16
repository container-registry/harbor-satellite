package async_test

import (
	"context"
	"encoding/json"
	"slices"
	"sync"
	"time"

	"github.com/container-registry/harbor-satellite/internal/groundcontrol/async"
	"github.com/container-registry/harbor-satellite/internal/groundcontrol/fleet"
)

// fakeOutbox is an in-memory outbox with the claim semantics of
// ClaimOutboxEvents: a claim counts as an attempt and leases the event.
type fakeOutbox struct {
	mu       sync.Mutex
	events   []async.OutboxEvent
	done     map[int64]bool
	leased   map[int64]bool
	claims   []int
	failures []failure
	releases []failure
	rearms   int
}

type failure struct {
	ID      int64
	Cause   string
	RetryIn time.Duration
}

func newFakeOutbox(kinds ...fleet.EventKind) *fakeOutbox {
	o := &fakeOutbox{done: map[int64]bool{}, leased: map[int64]bool{}}
	for i, k := range kinds {
		o.events = append(o.events, async.OutboxEvent{
			ID:        int64(i + 1),
			Kind:      k,
			Aggregate: fleet.Aggregate{Type: fleet.AggregateSatellite, ID: int64(i + 1)},
			Payload:   json.RawMessage(`{}`),
		})
	}
	return o
}

func (o *fakeOutbox) Claim(_ context.Context, batchSize, maxAttempts int, _ time.Duration) ([]async.OutboxEvent, error) {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.claims = append(o.claims, maxAttempts)
	var out []async.OutboxEvent
	for i := range o.events {
		e := &o.events[i]
		if len(out) == batchSize || o.done[e.ID] || o.leased[e.ID] || int(e.Attempts) >= maxAttempts {
			continue
		}
		e.Attempts++
		o.leased[e.ID] = true
		out = append(out, *e)
	}
	return out, nil
}

func (o *fakeOutbox) Complete(_ context.Context, ids []int64) error {
	o.mu.Lock()
	defer o.mu.Unlock()
	for _, id := range ids {
		o.done[id] = true
		delete(o.leased, id)
	}
	return nil
}

func (o *fakeOutbox) Fail(_ context.Context, id int64, cause string, retryIn time.Duration) error {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.failures = append(o.failures, failure{ID: id, Cause: cause, RetryIn: retryIn})
	delete(o.leased, id)
	return nil
}

func (o *fakeOutbox) Release(_ context.Context, id int64, cause string, retryIn time.Duration) error {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.releases = append(o.releases, failure{ID: id, Cause: cause, RetryIn: retryIn})
	delete(o.leased, id)
	for i := range o.events {
		if o.events[i].ID == id && o.events[i].Attempts > 0 {
			o.events[i].Attempts--
		}
	}
	return nil
}

func (o *fakeOutbox) Rearm(_ context.Context, maxAttempts int) (int64, error) {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.rearms++
	var n int64
	for i := range o.events {
		if !o.done[o.events[i].ID] && int(o.events[i].Attempts) >= maxAttempts {
			o.events[i].Attempts = 0
			n++
		}
	}
	return n, nil
}

// expire ends every lease, as time passing would.
func (o *fakeOutbox) expire() {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.leased = map[int64]bool{}
}

func (o *fakeOutbox) Stuck(context.Context, int, int) ([]async.OutboxEvent, error) {
	return nil, nil
}

func (o *fakeOutbox) Prune(context.Context, time.Duration, int) (int64, error) {
	return 0, nil
}

func (o *fakeOutbox) PendingCount(context.Context) (int64, error) {
	o.mu.Lock()
	defer o.mu.Unlock()
	return int64(len(o.events) - len(o.done)), nil
}

func (o *fakeOutbox) completed() []int64 {
	o.mu.Lock()
	defer o.mu.Unlock()
	var out []int64
	for id := range o.done {
		out = append(out, id)
	}
	slices.Sort(out)
	return out
}

// fakeDesired keeps desired-state rows with the semantics of
// RecordDesiredState and MarkDesiredStatePublished.
type fakeDesired struct {
	mu   sync.Mutex
	rows map[fleet.Aggregate]async.DesiredRecord
}

func newFakeDesired() *fakeDesired {
	return &fakeDesired{rows: map[fleet.Aggregate]async.DesiredRecord{}}
}

func (d *fakeDesired) Record(_ context.Context, aggregate fleet.Aggregate, document json.RawMessage, digest fleet.Digest) (int64, bool, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	r, ok := d.rows[aggregate]
	switch {
	case !ok:
		r = async.DesiredRecord{Aggregate: aggregate, Version: 1}
	case r.RenderedDigest != digest:
		r.Version++
	}
	r.Document, r.RenderedDigest = document, digest
	d.rows[aggregate] = r
	return r.Version, r.PublishedDigest != r.RenderedDigest, nil
}

func (d *fakeDesired) MarkPublished(_ context.Context, aggregate fleet.Aggregate, rendered, artifact fleet.Digest) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	if r, ok := d.rows[aggregate]; ok && r.RenderedDigest == rendered {
		r.PublishedDigest, r.PublishedArtifactDigest, r.LastError = rendered, artifact, ""
		d.rows[aggregate] = r
	}
	return nil
}

func (d *fakeDesired) ClearPublished(_ context.Context, aggregate fleet.Aggregate) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	if r, ok := d.rows[aggregate]; ok {
		r.PublishedDigest, r.PublishedArtifactDigest = "", ""
		d.rows[aggregate] = r
	}
	return nil
}

func (d *fakeDesired) MarkFailed(_ context.Context, aggregate fleet.Aggregate, cause string) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	if r, ok := d.rows[aggregate]; ok {
		r.LastError = cause
		d.rows[aggregate] = r
	}
	return nil
}

func (d *fakeDesired) Get(_ context.Context, aggregate fleet.Aggregate) (async.DesiredRecord, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	r, ok := d.rows[aggregate]
	if !ok {
		return async.DesiredRecord{}, fleet.ErrNotFound
	}
	return r, nil
}

func (d *fakeDesired) Delete(_ context.Context, aggregate fleet.Aggregate) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	delete(d.rows, aggregate)
	return nil
}

// fakeLocker holds aggregates the test marks as locked by another worker.
type fakeLocker struct {
	mu     sync.Mutex
	held   map[fleet.Aggregate]bool
	taken  map[fleet.Aggregate]int
	locked map[fleet.Aggregate]bool
}

func newFakeLocker() *fakeLocker {
	return &fakeLocker{held: map[fleet.Aggregate]bool{}, taken: map[fleet.Aggregate]int{}, locked: map[fleet.Aggregate]bool{}}
}

func (l *fakeLocker) TryLock(_ context.Context, aggregate fleet.Aggregate) (func(), bool, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.held[aggregate] || l.locked[aggregate] {
		return nil, false, nil
	}
	l.locked[aggregate] = true
	l.taken[aggregate]++
	return func() {
		l.mu.Lock()
		defer l.mu.Unlock()
		delete(l.locked, aggregate)
	}, true, nil
}

// fakeReachability is the registry probe as the test sets it.
type fakeReachability struct {
	mu        sync.Mutex
	reachable bool
}

func (r *fakeReachability) Reachable() bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.reachable
}

func (r *fakeReachability) set(reachable bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.reachable = reachable
}
