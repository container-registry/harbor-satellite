// Package budget counts bytes read from Harbor, a peer, or a direct-delivery
// pull. The heartbeat reports the count. A local OCI proxy pull is not counted.
package budget

import (
	"context"
	"io"
	"net/http"
	"sync"
	"time"
)

// Stats is the heartbeat snapshot. BytesPerSecond is the bytes read in the
// last second. BytesInWindow is the bytes read since the current state
// replication started. BytesTotal is every byte read by this process.
type Stats struct {
	BytesPerSecond float64
	BytesInWindow  int64
	BytesTotal     int64
}

type sample struct {
	at time.Time
	n  int64
}

// Budget records response bodies for the status report.
type Budget struct {
	mu            sync.Mutex
	bytesInWindow int64
	bytesTotal    int64
	samples       []sample
	now           func() time.Time
}

var (
	processOnce   sync.Once
	processBudget *Budget
)

// Ensure returns the one budget for this process.
func Ensure() *Budget {
	processOnce.Do(func() {
		processBudget = New()
	})
	return processBudget
}

// New returns a budget that is used by tests. Production callers use Ensure.
func New() *Budget {
	return &Budget{now: time.Now}
}

// BeginSync opens the reporting window for the state replication that is starting.
func (b *Budget) BeginSync() {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.bytesInWindow = 0
}

// Snapshot returns the recent rate and the byte counters.
func (b *Budget) Snapshot() Stats {
	b.mu.Lock()
	defer b.mu.Unlock()
	return Stats{
		BytesPerSecond: float64(recentBytes(b.samples, b.clock())),
		BytesInWindow:  b.bytesInWindow,
		BytesTotal:     b.bytesTotal,
	}
}

// Transport meters every response body. Direct delivery uses it because that
// client only talks to Harbor.
func Transport(base http.RoundTripper) http.RoundTripper {
	return newRoundTripper(base, true)
}

// ContextTransport meters a response body when the request context was marked
// with WithMeter. The registry client shared with the OCI proxy uses it, so a
// local pull is not counted.
func ContextTransport(base http.RoundTripper) http.RoundTripper {
	return newRoundTripper(base, false)
}

func newRoundTripper(base http.RoundTripper, always bool) http.RoundTripper {
	if base == nil {
		base = http.DefaultTransport
	}
	return roundTripper{base: base, always: always}
}

type roundTripper struct {
	base   http.RoundTripper
	always bool
}

func (r roundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	resp, err := r.base.RoundTrip(req)
	if err != nil || resp == nil || resp.Body == nil {
		return resp, err
	}
	if !r.always && !Metered(req.Context()) {
		return resp, nil
	}
	resp.Body = Ensure().wrapBody(req.Context(), resp.Body)
	return resp, nil
}

type meterContextKey struct{}

// WithMeter marks a replication download so the shared registry client counts
// it. The OCI proxy does not set this.
func WithMeter(ctx context.Context) context.Context {
	return context.WithValue(ctx, meterContextKey{}, true)
}

// Metered reports whether WithMeter marked the context.
func Metered(ctx context.Context) bool {
	if ctx == nil {
		return false
	}
	metered, ok := ctx.Value(meterContextKey{}).(bool)
	return ok && metered
}

func (b *Budget) wrapBody(ctx context.Context, body io.ReadCloser) io.ReadCloser {
	if body == nil {
		return nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	return &meteredBody{ctx: ctx, budget: b, body: body}
}

type meteredBody struct {
	ctx    context.Context
	budget *Budget
	body   io.ReadCloser
}

func (m *meteredBody) Read(p []byte) (int, error) {
	if err := m.ctx.Err(); err != nil {
		return 0, err
	}
	read, err := m.body.Read(p)
	if read > 0 {
		m.budget.record(int64(read))
	}
	return read, err
}

func (m *meteredBody) Close() error {
	return m.body.Close()
}

func (b *Budget) record(n int64) {
	if n <= 0 {
		return
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	now := b.clock()
	b.bytesTotal += n
	b.bytesInWindow += n
	b.samples = append(b.samples, sample{at: now, n: n})
	b.samples = trimSamples(b.samples, now)
}

func (b *Budget) clock() time.Time {
	if b.now != nil {
		return b.now()
	}
	return time.Now()
}

func trimSamples(samples []sample, now time.Time) []sample {
	cutoff := now.Add(-time.Second)
	drop := 0
	for drop < len(samples) && samples[drop].at.Before(cutoff) {
		drop++
	}
	if drop == 0 {
		return samples
	}
	return append([]sample(nil), samples[drop:]...)
}

func recentBytes(samples []sample, now time.Time) int64 {
	cutoff := now.Add(-time.Second)
	var total int64
	for _, sample := range samples {
		if !sample.at.Before(cutoff) {
			total += sample.n
		}
	}
	return total
}
