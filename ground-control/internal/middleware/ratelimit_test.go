package middleware

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// this block will check if constructor stores maxRequests, windowPeriod correctly.
// it also initializes the requests map.
func TestNewRateLimiter(t *testing.T) {
	rl := NewRateLimiter(10, time.Second)
	if rl.maxRequests != 10 {
		t.Errorf("expected maxRequests 10, got %d", rl.maxRequests)
	}
	if rl.windowPeriod != time.Second {
		t.Errorf("expected windowPeriod 1s, got %v", rl.windowPeriod)
	}
	if rl.requests == nil {
		t.Error("expected requests map to be initialized")
	}
}

// this sends exactly 3 requests to a limiter with max = 3, where all 3 should be allowed
func TestAllow_UnderLimit(t *testing.T) {
	rl := NewRateLimiter(3, time.Second)
	for i := range 3 {
		if !rl.Allow("1.2.3.4") {
			t.Errorf("expected request %d to be allowed", i+1)
		}
	}
}

// sends 3 requests to exhaust the limit and then sends the 4th and 4th must be blocked.
func TestAllow_AtLimit(t *testing.T) {
	rl := NewRateLimiter(3, time.Second)
	for range 3 {
		rl.Allow("1.2.3.4")
	}
	if rl.Allow("1.2.3.4") {
		t.Error("expected 4th request to be blocked")
	}
}

// exhausts the limit for 1.1.1.1 and then checks that 2.2.2.2 is still allowed
// It verifies that IP's are tracked independently and dont bleed into each other
func TestAllow_MultipleIPs(t *testing.T) {
	rl := NewRateLimiter(2, time.Second)
	rl.Allow("1.1.1.1")
	rl.Allow("1.1.1.1")
	if rl.Allow("1.1.1.1") {
		t.Error("expected 1.1.1.1 to be blocked")
	}
	if !rl.Allow("2.2.2.2") {
		t.Error("expected 2.2.2.2 to be allowed independently")
	}
}

// exhausts the limit, then sleeps 60ms past a 50ms window, after expiry the old timestamps are stale.
// doing this should allow a new request.
// this verifies the sliding window resets correctly
func TestAllow_WindowExpiry(t *testing.T) {
	rl := NewRateLimiter(2, 50*time.Millisecond)
	rl.Allow("1.2.3.4")
	rl.Allow("1.2.3.4")
	time.Sleep(60 * time.Millisecond)
	if !rl.Allow("1.2.3.4") {
		t.Error("expected request to be allowed after window expired")
	}
}

// wraps a dummy handler with the middleware, sends one request under the limit.
// expects the HTTP 200 to pass through to the real handler
func TestRateLimitMiddleware_Allowed(t *testing.T) {
	rl := NewRateLimiter(10, time.Second)
	handler := RateLimitMiddleware(rl)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.RemoteAddr = "1.2.3.4:5678"
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", rr.Code)
	}
}

// sends two requests to a max=1 limiter, second request should get HTTP 429
// with JSON BODY containing "error" : "Too many requests"
// Verifies both status code and response body.
func TestRateLimitMiddleware_Blocked(t *testing.T) {
	rl := NewRateLimiter(1, time.Second)
	handler := RateLimitMiddleware(rl)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.RemoteAddr = "1.2.3.4:5678"
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	rr = httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusTooManyRequests {
		t.Errorf("expected 429, got %d", rr.Code)
	}

	var body map[string]string
	if err := json.NewDecoder(rr.Body).Decode(&body); err != nil {
		t.Fatalf("failed to decode response body: %v", err)
	}
	if body["error"] != "Too Many Requests" {
		t.Errorf("unexpected error message: %s", body["error"])
	}
}

// this passed a normal host:port RemoteAddr
// expects just the host part back after SplitHostPort
func TestGetClientIP_RemoteAddr(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.RemoteAddr = "192.168.1.1:9090"
	ip := getClientIP(req)
	if ip != "192.168.1.1" {
		t.Errorf("expected 192.168.1.1, got %s", ip)
	}
}

// passes a bare IP with no PORT which makes the SplitHostPort fail
// Expects the fallback to return RemoteAddr string as-is
func TestGetClientIP_NoPort(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.RemoteAddr = "192.168.1.1"
	ip := getClientIP(req)
	if ip != "192.168.1.1" {
		t.Errorf("expected 192.168.1.1, got %s", ip)
	}
}
