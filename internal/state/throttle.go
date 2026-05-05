package state

import (
	"context"
	"io"
	"net/http"

	"golang.org/x/time/rate"
)

// throttledTransport wraps an http.RoundTripper and caps download throughput
// using a token-bucket limiter. Upload is not throttled because image pushes
// to the local Zot registry are loopback transfers.
type throttledTransport struct {
	base    http.RoundTripper
	limiter *rate.Limiter
}

// newThrottledTransport wraps base with a rate limiter set to mbps megabits/sec.
// Burst is set to 64 KiB or a quarter-second of transfer, whichever is larger,
// so normal HTTP read sizes (4-64 KiB) never exceed it.
func newThrottledTransport(base http.RoundTripper, mbps float64) *throttledTransport {
	bytesPerSec := mbps * 125_000 // 1 Mbps = 125,000 bytes/sec
	burst := int(bytesPerSec / 4)
	if burst < 64*1024 {
		burst = 64 * 1024
	}
	return &throttledTransport{
		base:    base,
		limiter: rate.NewLimiter(rate.Limit(bytesPerSec), burst),
	}
}

func (t *throttledTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	resp, err := t.base.RoundTrip(req)
	if err != nil {
		return nil, err
	}
	resp.Body = &throttledReader{body: resp.Body, limiter: t.limiter, ctx: req.Context()}
	return resp, nil
}

type throttledReader struct {
	body    io.ReadCloser
	limiter *rate.Limiter
	ctx     context.Context
}

func (r *throttledReader) Read(p []byte) (int, error) {
	n, err := r.body.Read(p)
	if n > 0 {
		// Clamp to burst so WaitN never returns an error for n > burst.
		// Oversized reads still consume tokens proportionally on the next call.
		tokens := n
		if tokens > r.limiter.Burst() {
			tokens = r.limiter.Burst()
		}
		if waitErr := r.limiter.WaitN(r.ctx, tokens); waitErr != nil {
			return n, waitErr
		}
	}
	return n, err
}

func (r *throttledReader) Close() error {
	return r.body.Close()
}
