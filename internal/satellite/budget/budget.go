// Package budget is the process-wide transfer budget. Speed limits how fast
// bytes are read from Harbor, a peer, or a direct-delivery pull. Quota skips
// an image when the bytes still to copy would exceed the open window.
package budget

import (
	"context"
	"errors"
	"io"
	"net/http"
	"sync"
	"time"

	"golang.org/x/time/rate"
)

const (
	// WindowSync resets at the start of each state replication.
	WindowSync Window = "sync"
	// WindowDay is the UTC calendar day.
	WindowDay Window = "day"
	// WindowWeek is the UTC ISO week.
	WindowWeek Window = "week"

	maxBurst = 4 << 20
)

// ErrQuotaExceeded means the image does not fit in the open quota window.
var ErrQuotaExceeded = errors.New("transfer quota exceeded")

// Window is sync, day, or week.
type Window string

// Speed is the token-bucket cap. BytesPerSecond is the sustained rate.
type Speed struct {
	Enabled        bool
	BytesPerSecond int64
}

// Quota is the data cap for one window.
type Quota struct {
	Enabled    bool
	LimitBytes int64
	Window     Window
}

// Policy is the live speed cap and data cap.
type Policy struct {
	Speed Speed
	Quota Quota
}

// Stats is the monitor snapshot.
type Stats struct {
	BytesPerSecond float64
	BytesInWindow  int64
	BytesTotal     int64
}

type sample struct {
	at time.Time
	n  int64
}

// Budget counts and slows response bodies. Admit only looks at the running
// total; Wrap is the only place bytes are recorded.
type Budget struct {
	mu            sync.Mutex
	policy        Policy
	limiter       *rate.Limiter
	windowStart   time.Time
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

// SetPolicy replaces the live limiter. An in-flight body observes the new
// rate on its next read. Switching the quota window starts a fresh count.
func (b *Budget) SetPolicy(policy Policy) {
	b.mu.Lock()
	defer b.mu.Unlock()

	windowChanged := b.policy.Quota.Enabled != policy.Quota.Enabled || b.policy.Quota.Window != policy.Quota.Window
	b.policy = policy
	if windowChanged {
		b.bytesInWindow = 0
		b.windowStart = b.clock().UTC()
	}
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

// BeginSync resets a sync quota. Day and week windows keep their count.
func (b *Budget) BeginSync() {
	b.mu.Lock()
	defer b.mu.Unlock()
	if !b.policy.Quota.Enabled || b.policy.Quota.Window != WindowSync {
		return
	}
	b.bytesInWindow = 0
	b.windowStart = b.clock().UTC()
}

// Admit reports whether missingBytes still fit in the open window. A disabled
// quota always fits. The bytes are not reserved; Wrap records what arrives.
func (b *Budget) Admit(missingBytes int64) error {
	if missingBytes < 0 {
		missingBytes = 0
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if !b.policy.Quota.Enabled {
		return nil
	}
	b.roll(b.clock())
	if b.bytesInWindow+missingBytes > b.policy.Quota.LimitBytes {
		return ErrQuotaExceeded
	}
	return nil
}

// Wrap records and slows a body. A disabled policy returns the body unchanged
// in behavior: reads are not delayed and bytes are not recorded.
func (b *Budget) Wrap(body io.ReadCloser) io.ReadCloser {
	return b.WrapContext(context.Background(), body)
}

// WrapContext is Wrap with the caller's context, so a canceled pull stops waiting.
func (b *Budget) WrapContext(ctx context.Context, body io.ReadCloser) io.ReadCloser {
	if body == nil {
		return nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	return &meteredBody{ctx: ctx, budget: b, body: body}
}

// Snapshot returns the current speed and the byte counters. Both knobs
// disabled reports zeros.
func (b *Budget) Snapshot() Stats {
	b.mu.Lock()
	defer b.mu.Unlock()
	if !b.policy.Speed.Enabled && !b.policy.Quota.Enabled {
		return Stats{}
	}
	now := b.clock()
	b.roll(now)
	return Stats{
		BytesPerSecond: float64(recentBytes(b.samples, now)),
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
// local pull is not capped.
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
	resp.Body = Ensure().WrapContext(req.Context(), resp.Body)
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

type meteredBody struct {
	ctx    context.Context
	budget *Budget
	body   io.ReadCloser
}

func (m *meteredBody) Read(p []byte) (int, error) {
	if len(p) == 0 {
		return 0, nil
	}
	chunk := m.budget.chunk(len(p))
	if chunk > 0 && chunk < len(p) {
		p = p[:chunk]
	}
	if chunk > 0 {
		if err := m.budget.wait(m.ctx, len(p)); err != nil {
			return 0, err
		}
	}
	n, err := m.body.Read(p)
	if n > 0 {
		m.budget.record(int64(n))
	}
	return n, err
}

func (m *meteredBody) Close() error {
	return m.body.Close()
}

// chunk is the next read size when speed is enabled, or 0 when it is not.
func (b *Budget) chunk(want int) int {
	b.mu.Lock()
	defer b.mu.Unlock()
	if !b.policy.Speed.Enabled || b.limiter == nil || want <= 0 {
		return 0
	}
	burst := b.limiter.Burst()
	if burst < 1 {
		burst = 1
	}
	if want > burst {
		return burst
	}
	return want
}

func (b *Budget) wait(ctx context.Context, n int) error {
	b.mu.Lock()
	limiter := b.limiter
	enabled := b.policy.Speed.Enabled
	b.mu.Unlock()
	if !enabled || limiter == nil || n <= 0 {
		return nil
	}
	if burst := limiter.Burst(); n > burst {
		n = burst
	}
	if n < 1 {
		return nil
	}
	return limiter.WaitN(ctx, n)
}

func (b *Budget) record(n int64) {
	if n <= 0 {
		return
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if !b.policy.Speed.Enabled && !b.policy.Quota.Enabled {
		return
	}
	now := b.clock()
	b.roll(now)
	b.bytesTotal += n
	if b.policy.Quota.Enabled {
		b.bytesInWindow += n
	}
	b.samples = append(b.samples, sample{at: now, n: n})
	b.samples = trimSamples(b.samples, now)
}

func (b *Budget) roll(now time.Time) {
	if !b.policy.Quota.Enabled || b.policy.Quota.Window == WindowSync || b.policy.Quota.Window == "" {
		return
	}
	if !windowChanged(b.policy.Quota.Window, b.windowStart, now) {
		return
	}
	b.bytesInWindow = 0
	b.windowStart = now.UTC()
}

func windowChanged(window Window, start, now time.Time) bool {
	if start.IsZero() {
		return true
	}
	switch window {
	case WindowSync:
		return false
	case WindowDay:
		return start.UTC().Year() != now.UTC().Year() || start.UTC().YearDay() != now.UTC().YearDay()
	case WindowWeek:
		startYear, startWeek := start.UTC().ISOWeek()
		nowYear, nowWeek := now.UTC().ISOWeek()
		return startYear != nowYear || startWeek != nowWeek
	default:
		return false
	}
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
