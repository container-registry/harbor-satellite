package webhook

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"strconv"
	"syscall"
	"time"

	"github.com/container-registry/harbor-satellite/internal/groundcontrol/fleet"
)

var (
	ErrInsecureURL = errors.New("webhook URL is http to a host that is not loopback")
	// ErrBlockedAddress covers the cloud metadata endpoints (ADR-0019).
	ErrBlockedAddress = errors.New("webhook URL resolves to a link-local address")
)

// HTTPSender posts the event with ContentType and the signature headers.
// It follows no redirects, refuses http for non-loopback hosts
// (ErrInsecureURL) and dials no link-local address (ErrBlockedAddress).
type HTTPSender struct {
	client *http.Client
	clock  fleet.Clock
}

var _ Sender = (*HTTPSender)(nil)

func NewHTTPSender(client *http.Client, clock fleet.Clock) *HTTPSender {
	if client == nil {
		client = &http.Client{Timeout: 10 * time.Second}
	}
	if clock == nil {
		clock = fleet.SystemClock{}
	}
	guarded := *client
	guarded.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	guarded.Transport = guardTransport(client.Transport)
	return &HTTPSender{client: &guarded, clock: clock}
}

// guardTransport refuses link-local peers at dial time, after DNS, so a
// name that resolves to a metadata endpoint is refused too.
func guardTransport(rt http.RoundTripper) http.RoundTripper {
	base, ok := rt.(*http.Transport)
	if rt == nil || !ok {
		def, ok := http.DefaultTransport.(*http.Transport)
		if !ok {
			return rt
		}
		base = def
	}
	t := base.Clone()
	dialer := &net.Dialer{Timeout: 10 * time.Second, Control: func(_, address string, _ syscall.RawConn) error {
		host, _, err := net.SplitHostPort(address)
		if err != nil {
			return err
		}
		if addr, err := netip.ParseAddr(host); err == nil && blocked(addr) {
			return fmt.Errorf("%w: %s", ErrBlockedAddress, addr)
		}
		return nil
	}}
	t.DialContext = dialer.DialContext
	t.Proxy = nil
	return t
}

func blocked(addr netip.Addr) bool { return fleet.BlockedWebhookAddr(addr) }

func isLoopbackHost(host string) bool {
	if host == "localhost" {
		return true
	}
	addr, err := netip.ParseAddr(host)
	return err == nil && addr.Unmap().IsLoopback()
}

// CheckURL is the rule Send applies, for validating a subscription.
func CheckURL(raw string) error {
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" {
		return fmt.Errorf("webhook URL %q is not an absolute URL", raw)
	}
	switch u.Scheme {
	case "https":
	case "http":
		if !isLoopbackHost(u.Hostname()) {
			return ErrInsecureURL
		}
	default:
		return fmt.Errorf("webhook URL scheme %q: want https", u.Scheme)
	}
	if addr, err := netip.ParseAddr(u.Hostname()); err == nil && blocked(addr) {
		return fmt.Errorf("%w: %s", ErrBlockedAddress, addr)
	}
	return nil
}

func (s *HTTPSender) Send(ctx context.Context, url string, secret []byte, event CloudEvent) (int, error) {
	if err := CheckURL(url); err != nil {
		return 0, err
	}
	body, err := json.Marshal(event)
	if err != nil {
		return 0, fmt.Errorf("encode CloudEvent %s: %w", event.ID, err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return 0, err
	}
	now := s.clock.Now()
	req.Header.Set("Content-Type", ContentType)
	req.Header.Set(TimestampHeader, strconv.FormatInt(now.Unix(), 10))
	req.Header.Set(SignatureHeader, Sign(secret, now, body))
	req.Header.Set("Idempotency-Key", event.ID)
	resp, err := s.client.Do(req)
	if err != nil {
		return 0, err
	}
	defer func() { _ = resp.Body.Close() }()
	// Draining lets the connection be reused; a short read changes nothing
	// about the delivery.
	if _, err := io.Copy(io.Discard, io.LimitReader(resp.Body, 1<<16)); err != nil {
		return resp.StatusCode, nil
	}
	return resp.StatusCode, nil
}
