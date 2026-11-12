// Package budget is the process-wide download speed cap. It slows bytes read
// from Harbor, a peer, or a direct-delivery pull. A disabled cap does not
// delay a read. Bytes are still recorded so a later heartbeat can report
// throughput.
package budget

import (
	"context"
	"io"
	"net/http"
	"sync"
	"time"

	"golang.org/x/time/rate"
)

const maxBurst = 4 << 20

// Speed is the token-bucket cap. BytesPerSecond is the sustained rate.
type Speed struct {
	Enabled        bool
	BytesPerSecond int64
}

// Policy is the live speed cap.
type Policy struct {
	Speed Speed
}

// Stats is the throughput snapshot.
type Stats struct {
	BytesPerSecond float64
	BytesTotal     int64
}

type sample struct {
	at time.Time
	n  int64
}

// Budget slows response bodies and counts the bytes that arrive.
type Budget struct {
	mu         sync.Mutex
	policy     Policy
	limiter    *rate.Limiter
	bytesTotal int64
	samples    []sample
	now        func() time.Time
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

// SetPolicy replaces the live limiter. An in-flight body observes the new
// rate on its next read.
func (b *Budget) SetPolicy(policy Policy) {
	b.mu.Lock()
	defer b.mu.Unlock()

	b.policy = policy
	if !policy.Speed.Enabled || policy.Speed.BytesPerSecond <= 0 {
		b.limiter = nil
		return
	}

	burst := int(policy.Speed.BytesPerSecond)
	if policy.Speed.BytesPerSecond > maxBurst {
		burst = maxBurst
	}
	if burst < 1 {
		burst = 1
	}
	limit := rate.Limit(policy.Speed.BytesPerSecond)
	if b.limiter == nil {
		b.limiter = rate.NewLimiter(limit, burst)
		return
	}
	b.limiter.SetLimit(limit)
	b.limiter.SetBurst(burst)
}

// Wrap records and slows a body. A disabled speed limit does not delay the read.
func (b *Budget) Wrap(body io.ReadCloser) io.ReadCloser {
	return b.WrapContext(context.Background(), body)
}

// WrapContext is Wrap with the caller's context, so a canceled pull stops waiting.
func (b *Budget) WrapContext(ctx context.Context, body io.ReadCloser) io.ReadCloser {
	return b.wrapBody(ctx, body, true)
}

// Snapshot returns the recent throughput and the total bytes read.
func (b *Budget) Snapshot() Stats {
	b.mu.Lock()
	defer b.mu.Unlock()
	now := b.clock()
	return Stats{
		BytesPerSecond: float64(recentBytes(b.samples, now)),
		BytesTotal:     b.bytesTotal,
	}
}

// Transport meters every response body. Direct delivery uses it because that
// client only talks to Harbor.
func Transport(base http.RoundTripper) http.RoundTripper {
	return newRoundTripper(base, true, true)
}

// ContextTransport meters a response body when the request context was marked
// with WithMeter. The registry client shared with the OCI proxy uses it, so a
// local pull is not capped.
func ContextTransport(base http.RoundTripper) http.RoundTripper {
	return newRoundTripper(base, false, true)
}

func newRoundTripper(base http.RoundTripper, always, count bool) http.RoundTripper {
	if base == nil {
		base = http.DefaultTransport
	}
	return roundTripper{base: base, always: always, count: count}
}

type roundTripper struct {
	base   http.RoundTripper
	always bool
	count  bool
}

func (r roundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	resp, err := r.base.RoundTrip(req)
	if err != nil || resp == nil || resp.Body == nil {
		return resp, err
	}
	if !r.always && !Metered(req.Context()) {
		return resp, nil
	}
	resp.Body = Ensure().wrapBody(req.Context(), resp.Body, r.count)
	return resp, nil
}

type meterContextKey struct{}

// WithMeter marks a replication download so the shared registry client counts
// and slows it. The OCI proxy does not set this.
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

func (b *Budget) wrapBody(ctx context.Context, body io.ReadCloser, count bool) io.ReadCloser {
	if body == nil {
		return nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	return &meteredBody{ctx: ctx, budget: b, body: body, count: count}
}

type meteredBody struct {
	ctx    context.Context
	budget *Budget
	body   io.ReadCloser
	count  bool
}

func (m *meteredBody) Read(p []byte) (int, error) {
	if len(p) == 0 {
		return 0, nil
	}
	// The limiter and the read size come from one snapshot. A config reload
	// between them would wait for the new rate and still read the old size.
	limiter, n := m.budget.prepareRead(len(p))
	if n < len(p) {
		p = p[:n]
	}
	if limiter != nil {
		if err := limiter.WaitN(m.ctx, len(p)); err != nil {
			return 0, err
		}
	}
	read, err := m.body.Read(p)
	if read > 0 && m.count {
		m.budget.record(int64(read))
	}
	return read, err
}

func (m *meteredBody) Close() error {
	return m.body.Close()
}

// prepareRead returns the limiter to wait on and the number of bytes this
// read may take. A nil limiter means the speed cap is off.
func (b *Budget) prepareRead(want int) (*rate.Limiter, int) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if want < 1 {
		return nil, 0
	}
	if !b.policy.Speed.Enabled || b.limiter == nil {
		return nil, want
	}
	burst := b.limiter.Burst()
	if burst < 1 {
		burst = 1
	}
	if want > burst {
		want = burst
	}
	return b.limiter, want
}

func (b *Budget) record(n int64) {
	if n <= 0 {
		return
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	now := b.clock()
	b.bytesTotal += n
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
