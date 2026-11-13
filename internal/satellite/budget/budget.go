// Package budget is the process-wide transfer quota. It counts bytes read
// from Harbor, a peer, or a direct-delivery pull and skips an image when the
// bytes still to copy would exceed the open window.
package budget

import (
	"context"
	"errors"
	"io"
	"net/http"
	"sync"
	"time"
)

const (
	// WindowSync resets at the start of each state replication.
	WindowSync Window = "sync"
	// WindowDay is the UTC calendar day.
	WindowDay Window = "day"
	// WindowWeek is the UTC ISO week.
	WindowWeek Window = "week"
)

// ErrQuotaExceeded means the image does not fit in the open quota window.
var ErrQuotaExceeded = errors.New("transfer quota exceeded")

// Window is sync, day, or week.
type Window string

// Quota is the data cap for one window.
type Quota struct {
	Enabled    bool
	LimitBytes int64
	Window     Window
}

// Policy is the live data cap.
type Policy struct {
	Quota Quota
}

// Stats is the byte counters. The window counter stays at zero while the
// quota is disabled.
type Stats struct {
	BytesInWindow int64
	BytesTotal    int64
}

// Budget counts response bodies. Admit only looks at the running total; Wrap
// is the only place bytes are recorded.
type Budget struct {
	mu            sync.Mutex
	policy        Policy
	windowStart   time.Time
	bytesInWindow int64
	bytesTotal    int64
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

// SetPolicy replaces the live quota. Switching the window starts a fresh count.
func (b *Budget) SetPolicy(policy Policy) {
	b.mu.Lock()
	defer b.mu.Unlock()

	windowChanged := b.policy.Quota.Enabled != policy.Quota.Enabled || b.policy.Quota.Window != policy.Quota.Window
	b.policy = policy
	if windowChanged {
		b.bytesInWindow = 0
		b.windowStart = b.clock().UTC()
	}
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

// QuotaEnabled reports whether Admit can reject an image.
func (b *Budget) QuotaEnabled() bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.policy.Quota.Enabled
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
	// A copy that adds nothing still fits, including when the window is
	// already over the limit. Subtracting avoids wrapping the sum.
	if missingBytes > 0 && missingBytes > b.policy.Quota.LimitBytes-b.bytesInWindow {
		return ErrQuotaExceeded
	}
	return nil
}

// Wrap records a body. A disabled quota still counts the lifetime total and
// leaves the window counter at zero.
func (b *Budget) Wrap(body io.ReadCloser) io.ReadCloser {
	return b.WrapContext(context.Background(), body)
}

// WrapContext is Wrap with the caller's context.
func (b *Budget) WrapContext(ctx context.Context, body io.ReadCloser) io.ReadCloser {
	return b.wrapBody(ctx, body, true)
}

// Snapshot returns the byte counters. The window counter stays at zero while
// the quota is disabled.
func (b *Budget) Snapshot() Stats {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.roll(b.clock())
	return Stats{
		BytesInWindow: b.bytesInWindow,
		BytesTotal:    b.bytesTotal,
	}
}

// Transport meters every response body. Direct delivery uses it for the pull
// that happens after Admit.
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
	resp.Body = Ensure().wrapBody(req.Context(), resp.Body, true)
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
	if err := m.ctx.Err(); err != nil {
		return 0, err
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

func (b *Budget) record(n int64) {
	if n <= 0 {
		return
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	b.roll(b.clock())
	b.bytesTotal += n
	if b.policy.Quota.Enabled {
		b.bytesInWindow += n
	}
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
