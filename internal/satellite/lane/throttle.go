package lane

import (
	"context"
	"io"
	"net/http"
	"sync"
	"time"
)

// ThrottledTransport caps the bytes base moves per second, request and
// response bodies together, across every request it carries (plan item
// 6.2, replication_bandwidth_bytes_per_sec). It may burst one second's
// worth. A registry store applies it for
// store.RegistryOptions.BandwidthBytesPerSec, inside oras's retry
// transport, so retries spend the budget too. A bytesPerSec of zero or
// less means no cap and returns base.
func ThrottledTransport(base http.RoundTripper, bytesPerSec int64) http.RoundTripper {
	if bytesPerSec <= 0 {
		return base
	}
	if base == nil {
		base = http.DefaultTransport
	}
	rate := float64(bytesPerSec)
	return &throttledTransport{base: base, bucket: &tokenBucket{rate: rate, burst: rate, tokens: rate, last: time.Now()}}
}

type throttledTransport struct {
	base   http.RoundTripper
	bucket *tokenBucket
}

func (t *throttledTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	if req.Body != nil && req.Body != http.NoBody {
		clone := req.Clone(req.Context())
		clone.Body = &throttledBody{ReadCloser: req.Body, ctx: req.Context(), bucket: t.bucket}
		req = clone
	}
	resp, err := t.base.RoundTrip(req)
	if err != nil {
		return nil, err
	}
	if resp.Body != nil && resp.Body != http.NoBody {
		resp.Body = &throttledBody{ReadCloser: resp.Body, ctx: req.Context(), bucket: t.bucket}
	}
	return resp, nil
}

type throttledBody struct {
	io.ReadCloser
	ctx    context.Context
	bucket *tokenBucket
}

func (b *throttledBody) Read(p []byte) (int, error) {
	n, err := b.ReadCloser.Read(p)
	if n > 0 {
		if werr := b.bucket.take(b.ctx, n); werr != nil {
			return n, werr
		}
	}
	return n, err
}

// tokenBucket lets tokens go negative: a reader that took more than is
// there sleeps until the debt is paid, which keeps one large read exact.
type tokenBucket struct {
	mu                  sync.Mutex
	rate, burst, tokens float64
	last                time.Time
}

func (b *tokenBucket) take(ctx context.Context, n int) error {
	b.mu.Lock()
	now := time.Now()
	b.tokens = min(b.burst, b.tokens+now.Sub(b.last).Seconds()*b.rate)
	b.last = now
	b.tokens -= float64(n)
	var wait time.Duration
	if b.tokens < 0 {
		wait = time.Duration(-b.tokens / b.rate * float64(time.Second))
	}
	b.mu.Unlock()
	if wait <= 0 {
		return nil
	}
	timer := time.NewTimer(wait)
	defer timer.Stop()
	select {
	case <-timer.C:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
