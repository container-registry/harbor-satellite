package server

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestClientIP(t *testing.T) {
	tests := []struct {
		name   string
		trust  bool
		hops   int
		xff    []string
		realIP string
		want   string
	}{
		{name: "headers ignored without trust", xff: []string{"203.0.113.9"}, realIP: "203.0.113.8", want: "192.0.2.1"},
		{name: "single entry", trust: true, hops: 1, xff: []string{"203.0.113.9"}, want: "203.0.113.9"},
		{name: "rightmost entry with one proxy", trust: true, hops: 1, xff: []string{"6.6.6.6, 203.0.113.9"}, want: "203.0.113.9"},
		{name: "zero hops count as one", trust: true, hops: 0, xff: []string{"6.6.6.6, 203.0.113.9"}, want: "203.0.113.9"},
		{name: "two proxies", trust: true, hops: 2, xff: []string{"6.6.6.6, 203.0.113.9, 198.51.100.7"}, want: "203.0.113.9"},
		{name: "header lines read as one list", trust: true, hops: 2, xff: []string{"6.6.6.6, 203.0.113.9", "198.51.100.7"}, want: "203.0.113.9"},
		{name: "fewer entries than hops", trust: true, hops: 3, xff: []string{"203.0.113.9, 198.51.100.7"}, want: "203.0.113.9"},
		{name: "empty entries skipped", trust: true, hops: 1, xff: []string{"203.0.113.9, ,"}, want: "203.0.113.9"},
		{name: "X-Real-IP without X-Forwarded-For", trust: true, hops: 1, realIP: "203.0.113.8", want: "203.0.113.8"},
		{name: "peer without headers", trust: true, hops: 1, want: "192.0.2.1"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := &Server{trustForwardedHeaders: tt.trust, trustedProxyHops: tt.hops}
			r := httptest.NewRequest(http.MethodGet, "/", nil)
			r.RemoteAddr = "192.0.2.1:4711"
			for _, v := range tt.xff {
				r.Header.Add("X-Forwarded-For", v)
			}
			if tt.realIP != "" {
				r.Header.Set("X-Real-IP", tt.realIP)
			}
			require.Equal(t, tt.want, s.clientIP(r))
		})
	}
}

// A client that writes a new leftmost X-Forwarded-For entry on every
// request still shares the bucket of the address its proxy appended.
func TestAnonymousLimiter_ForgedLeftmostForwardedEntryDoesNotEscape(t *testing.T) {
	splitLimits(t, "2/1m", "3/1s")
	t.Setenv("AUDIT_TRUST_FORWARDED_HEADERS", "true")
	h := newGCHarness(t)
	ztr := func(i int) int {
		t.Helper()
		req := h.newRequest(http.MethodPost, "/satellites/ztr", ZTRRequest{Token: "unknown"}, nil)
		req.Header.Set("X-Forwarded-For", fmt.Sprintf("10.9.0.%d, 10.0.0.1", i))
		r, err := send(req)
		require.NoError(t, err)
		return r.Status
	}

	for i := range 2 {
		require.NotEqual(t, http.StatusTooManyRequests, ztr(i))
	}
	require.Equal(t, http.StatusTooManyRequests, ztr(3))
}

func TestNewServer_RejectsTrustedProxyHopsBelowOne(t *testing.T) {
	t.Setenv("AUDIT_TRUSTED_PROXY_HOPS", "0")
	loadGCForTest(t)
	_, err := NewServer(context.Background())
	require.ErrorContains(t, err, "AUDIT_TRUSTED_PROXY_HOPS")
}

func TestNewServer_RejectsNonPositiveAuthFailureWindow(t *testing.T) {
	for _, window := range []string{"0s", "-1m"} {
		t.Run(window, func(t *testing.T) {
			t.Setenv("AUTH_FAILURE_LIMIT", "5")
			t.Setenv("AUTH_FAILURE_WINDOW", window)
			loadGCForTest(t)
			_, err := NewServer(context.Background())
			require.ErrorContains(t, err, "AUTH_FAILURE_WINDOW")
		})
	}
}

func TestNewServer_AcceptsZeroAuthFailureWindowWhenLimitDisabled(t *testing.T) {
	t.Setenv("AUTH_FAILURE_LIMIT", "0")
	t.Setenv("AUTH_FAILURE_WINDOW", "0s")
	t.Setenv("OIDC_ROLE_MAP", "ops=bogus")
	loadGCForTest(t)
	_, err := NewServer(context.Background())
	require.ErrorContains(t, err, "OIDC_ROLE_MAP", "validation goes past the failure window")
}
