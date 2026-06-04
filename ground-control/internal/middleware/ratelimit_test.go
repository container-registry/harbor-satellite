package middleware

import (
	"net/http"
	"testing"
	"time"
)

func TestGetClientIP(t *testing.T) {
	// Setup limiter with trusted proxies: one explicit IP, one CIDR block
	trustedProxies := []string{"10.0.0.1", "192.168.1.0/24"}
	rl := NewRateLimiter(10, time.Minute, trustedProxies)

	tests := []struct {
		name       string
		remoteAddr string
		xffHeaders []string
		expected   string
	}{
		{
			name:       "Direct connection, no proxies",
			remoteAddr: "203.0.113.1:12345",
			expected:   "203.0.113.1",
		},
		{
			name:       "Direct connection spoofing XFF",
			remoteAddr: "203.0.113.1:12345",
			xffHeaders: []string{"1.1.1.1"},
			expected:   "203.0.113.1", // Ignored XFF because RemoteAddr isn't trusted
		},
		{
			name:       "One trusted proxy",
			remoteAddr: "10.0.0.1:54321", // Trusted explicit IP
			xffHeaders: []string{"203.0.113.1"},
			expected:   "203.0.113.1",
		},
		{
			name:       "Trusted proxy via CIDR",
			remoteAddr: "192.168.1.100:8080", // Trusted via CIDR
			xffHeaders: []string{"203.0.113.1"},
			expected:   "203.0.113.1",
		},
		{
			name:       "Multiple proxies (one trusted, one untrusted)",
			remoteAddr: "10.0.0.1:1234",
			xffHeaders: []string{"203.0.113.1, 198.51.100.1"},
			// 10.0.0.1 is trusted. Evaluates right-to-left:
			// 198.51.100.1 is not trusted, so we stop here.
			expected: "198.51.100.1",
		},
		{
			name:       "Multiple trusted proxies",
			remoteAddr: "10.0.0.1:1234",
			xffHeaders: []string{"203.0.113.1, 192.168.1.50"},
			// 10.0.0.1 is trusted. Evaluates right-to-left:
			// 192.168.1.50 IS trusted, keep going.
			// 203.0.113.1 is not trusted, so we stop here.
			expected: "203.0.113.1",
		},
		{
			name:       "Spoofed client IP through trusted proxy",
			remoteAddr: "10.0.0.1:1234",
			xffHeaders: []string{"1.1.1.1, 203.0.113.1"},
			// Attacker tries to spoof 1.1.1.1
			// 10.0.0.1 trusted -> evaluates right to left.
			// 203.0.113.1 is not trusted. We stop and return 203.0.113.1.
			expected: "203.0.113.1",
		},
		{
			name:       "Multi-header XFF bypass attempt",
			remoteAddr: "10.0.0.1:1234",
			// Attacker sends first header, proxy appends second header
			xffHeaders: []string{"1.1.1.1", "203.0.113.1"},
			// Both headers are joined and parsed right-to-left.
			// 203.0.113.1 is not trusted, so we stop and return it.
			expected: "203.0.113.1",
		},
		{
			name:       "Invalid hop returns RemoteAddr",
			remoteAddr: "10.0.0.1:1234",
			xffHeaders: []string{"garbage, 192.168.1.50"},
			// 192.168.1.50 is trusted, keep going.
			// "garbage" is invalid, return original RemoteAddr.
			expected: "10.0.0.1",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req, _ := http.NewRequest("GET", "/", nil)
			req.RemoteAddr = tt.remoteAddr
			for _, xff := range tt.xffHeaders {
				req.Header.Add("X-Forwarded-For", xff)
			}

			ip := rl.getClientIP(req)
			if ip != tt.expected {
				t.Errorf("expected %s, got %s", tt.expected, ip)
			}
		})
	}
}

func TestGetClientIPNoTrustedProxies(t *testing.T) {
	rl := NewRateLimiter(10, time.Minute, nil)

	req, _ := http.NewRequest("GET", "/", nil)
	req.RemoteAddr = "203.0.113.1:12345"
	req.Header.Set("X-Forwarded-For", "1.1.1.1")

	ip := rl.getClientIP(req)
	if ip != "203.0.113.1" {
		t.Errorf("expected 203.0.113.1, got %s", ip)
	}
}
