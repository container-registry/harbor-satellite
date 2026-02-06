package token

import (
	"sync"
	"time"
)

// TokenStore tracks token usage for single-use enforcement.
type TokenStore interface {
	// MarkUsed marks a token as used and returns error if already used.
	MarkUsed(tokenID string) error
	// IsUsed checks if a token has been used.
	IsUsed(tokenID string) bool
	// CheckRateLimit checks if the IP has exceeded rate limit.
	CheckRateLimit(ip string) error
	// RecordAttempt records a token attempt for rate limiting.
	RecordAttempt(ip string)
}

// MemoryTokenStore is an in-memory implementation of TokenStore.
type MemoryTokenStore struct {
	mu           sync.RWMutex
	usedTokens   map[string]time.Time
	rateLimits   map[string][]time.Time
	maxAttempts  int
	windowPeriod time.Duration
}

// NewMemoryTokenStore creates a new in-memory token store.
func NewMemoryTokenStore(maxAttempts int, windowPeriod time.Duration) *MemoryTokenStore {
	return &MemoryTokenStore{
		usedTokens:   make(map[string]time.Time),
		rateLimits:   make(map[string][]time.Time),
		maxAttempts:  maxAttempts,
		windowPeriod: windowPeriod,
	}
}

func (s *MemoryTokenStore) MarkUsed(tokenID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if _, exists := s.usedTokens[tokenID]; exists {
		return ErrTokenAlreadyUsed
	}

	s.usedTokens[tokenID] = time.Now()
	return nil
}

func (s *MemoryTokenStore) IsUsed(tokenID string) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()

	_, exists := s.usedTokens[tokenID]
	return exists
}

func (s *MemoryTokenStore) CheckRateLimit(ip string) error {
	s.mu.RLock()
	defer s.mu.RUnlock()

	attempts := s.rateLimits[ip]
	cutoff := time.Now().Add(-s.windowPeriod)

	count := 0
	for _, t := range attempts {
		if t.After(cutoff) {
			count++
		}
	}

	if count >= s.maxAttempts {
		return ErrTokenRateLimited
	}

	return nil
}

func (s *MemoryTokenStore) RecordAttempt(ip string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	cutoff := time.Now().Add(-s.windowPeriod)

	var filtered []time.Time
	for _, t := range s.rateLimits[ip] {
		if t.After(cutoff) {
			filtered = append(filtered, t)
		}
	}

	s.rateLimits[ip] = append(filtered, time.Now())
}

// Cleanup removes expired tokens and rate limit entries.
func (s *MemoryTokenStore) Cleanup() {
	s.mu.Lock()
	defer s.mu.Unlock()

	cutoff := time.Now().Add(-s.windowPeriod)

	for ip, attempts := range s.rateLimits {
		var filtered []time.Time
		for _, t := range attempts {
			if t.After(cutoff) {
				filtered = append(filtered, t)
			}
		}
		if len(filtered) == 0 {
			delete(s.rateLimits, ip)
		} else {
			s.rateLimits[ip] = filtered
		}
	}
}

// MockTokenStore is a mock implementation for testing.
type MockTokenStore struct {
	UsedTokens   map[string]bool
	RateLimited  map[string]bool
	MarkUsedErr  error
	RateLimitErr error
}

// NewMockTokenStore creates a mock token store.
func NewMockTokenStore() *MockTokenStore {
	return &MockTokenStore{
		UsedTokens:  make(map[string]bool),
		RateLimited: make(map[string]bool),
	}
}

func (m *MockTokenStore) MarkUsed(tokenID string) error {
	if m.MarkUsedErr != nil {
		return m.MarkUsedErr
	}
	if m.UsedTokens[tokenID] {
		return ErrTokenAlreadyUsed
	}
	m.UsedTokens[tokenID] = true
	return nil
}

func (m *MockTokenStore) IsUsed(tokenID string) bool {
	return m.UsedTokens[tokenID]
}

func (m *MockTokenStore) CheckRateLimit(ip string) error {
	if m.RateLimitErr != nil {
		return m.RateLimitErr
	}
	if m.RateLimited[ip] {
		return ErrTokenRateLimited
	}
	return nil
}

func (m *MockTokenStore) RecordAttempt(ip string) {}
