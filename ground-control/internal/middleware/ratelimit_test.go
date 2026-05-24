package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestAllow_UnderLimit(t *testing.T) {
	rl := NewRateLimiter(5, time.Minute)
	for i := 0; i < 5; i++ {
		if !rl.Allow("192.168.1.1") {
			t.Errorf("expected request %d to be allowed", i+1)
		}
	}
}

func TestAllow_ExceedsLimit(t *testing.T) {
	rl := NewRateLimiter(3, time.Minute)
	for i := 0; i < 3; i++ {
		rl.Allow("10.0.0.1")
	}
	if rl.Allow("10.0.0.1") {
		t.Error("expected 4th request to be blocked")
	}
}

func TestAllow_DifferentIPsAreIndependent(t *testing.T) {
	rl := NewRateLimiter(2, time.Minute)
	rl.Allow("1.1.1.1")
	rl.Allow("1.1.1.1")
	if rl.Allow("1.1.1.1") {
		t.Error("expected 3rd request from 1.1.1.1 to be blocked")
	}
	if !rl.Allow("2.2.2.2") {
		t.Error("expected request from 2.2.2.2 to be allowed")
	}
}

func TestAllow_WindowExpiry(t *testing.T) {
	rl := NewRateLimiter(2, 100*time.Millisecond)
	rl.Allow("10.0.0.1")
	rl.Allow("10.0.0.1")
	if rl.Allow("10.0.0.1") {
		t.Error("expected request to be blocked before window expires")
	}
	time.Sleep(150 * time.Millisecond)
	if !rl.Allow("10.0.0.1") {
		t.Error("expected request to be allowed after window expires")
	}
}

func TestRateLimitMiddleware_Allows(t *testing.T) {
	rl := NewRateLimiter(10, time.Minute)
	handler := RateLimitMiddleware(rl)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.RemoteAddr = "127.0.0.1:5000"
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", w.Code)
	}
}

func TestRateLimitMiddleware_Blocks(t *testing.T) {
	rl := NewRateLimiter(2, time.Minute)
	handler := RateLimitMiddleware(rl)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	for i := 0; i < 2; i++ {
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		req.RemoteAddr = "127.0.0.1:5000"
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, req)
	}

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.RemoteAddr = "127.0.0.1:5000"
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusTooManyRequests {
		t.Errorf("expected 429, got %d", w.Code)
	}
}

func TestRateLimitMiddleware_BlockedResponseBody(t *testing.T) {
	rl := NewRateLimiter(1, time.Minute)
	handler := RateLimitMiddleware(rl)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	for i := 0; i < 2; i++ {
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		req.RemoteAddr = "10.0.0.1:9000"
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, req)
	}

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.RemoteAddr = "10.0.0.1:9000"
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Header().Get("Content-Type") != "application/json" {
		t.Errorf("expected application/json, got %s", w.Header().Get("Content-Type"))
	}
}

func TestGetClientIP_ValidRemoteAddr(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.RemoteAddr = "192.168.0.1:8080"
	ip := getClientIP(req)
	if ip != "192.168.0.1" {
		t.Errorf("expected 192.168.0.1, got %s", ip)
	}
}

func TestGetClientIP_InvalidRemoteAddr(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.RemoteAddr = "invalid"
	ip := getClientIP(req)
	if ip != "invalid" {
		t.Errorf("expected fallback to raw RemoteAddr, got %s", ip)
	}
}
