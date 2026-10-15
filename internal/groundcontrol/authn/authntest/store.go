// Package authntest has a recording authn.SessionStore.
package authntest

import (
	"context"
	"crypto/sha256"
	"sync"
	"time"

	"github.com/container-registry/harbor-satellite/internal/groundcontrol/authn"
	"github.com/container-registry/harbor-satellite/internal/groundcontrol/fleet"
)

var _ authn.SessionStore = (*Store)(nil)

// Call is one recorded store call; Args are the arguments after the
// context.
type Call struct {
	Method string
	Args   []any
}

// Store records every call and answers from fields the test may change
// between calls. Writes change nothing.
type Store struct {
	mu    sync.Mutex
	calls []Call
	fails map[string]error
	// Auth answers FindByHash; nil is fleet.ErrNotFound.
	Auth map[[sha256.Size]byte]fleet.SatelliteAuth
	// ByName answers FindByName; a missing name is fleet.ErrNotFound.
	ByName map[string]fleet.SatelliteAuth
	// AddIfNoneOK is what AddIfNone returns.
	AddIfNoneOK bool
	// Expired is what DeleteExpired returns.
	Expired int64
}

func NewStore() *Store {
	return &Store{Auth: map[[sha256.Size]byte]fleet.SatelliteAuth{}, ByName: map[string]fleet.SatelliteAuth{}}
}

// SetAuth changes the answer for hash under the lock, for tests that
// change answers while another goroutine reads.
func (s *Store) SetAuth(hash [sha256.Size]byte, auth fleet.SatelliteAuth) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.Auth[hash] = auth
}

// Forget removes the answer for hash, as a deleted session would.
func (s *Store) Forget(hash [sha256.Size]byte) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.Auth, hash)
}

// SetByName changes the answer for name under the lock.
func (s *Store) SetByName(name string, auth fleet.SatelliteAuth) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.ByName[name] = auth
}

// FailOn makes every later call of method return err.
func (s *Store) FailOn(method string, err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.fails == nil {
		s.fails = map[string]error{}
	}
	s.fails[method] = err
}

func (s *Store) Calls() []Call {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]Call(nil), s.calls...)
}

// Count is the number of calls of method.
func (s *Store) Count(method string) int {
	n := 0
	for _, c := range s.Calls() {
		if c.Method == method {
			n++
		}
	}
	return n
}

func (s *Store) record(method string, args ...any) error {
	s.calls = append(s.calls, Call{Method: method, Args: args})
	return s.fails[method]
}

func (s *Store) Replace(_ context.Context, satellite fleet.SatelliteID, hash [sha256.Size]byte, expiresAt time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.record("Replace", satellite, hash, expiresAt)
}

func (s *Store) AddIfNone(_ context.Context, satellite fleet.SatelliteID, hash [sha256.Size]byte, expiresAt, now time.Time) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.record("AddIfNone", satellite, hash, expiresAt, now); err != nil {
		return false, err
	}
	return s.AddIfNoneOK, nil
}

func (s *Store) Renew(_ context.Context, satellite fleet.SatelliteID, from int64, hash [sha256.Size]byte, expiresAt time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.record("Renew", satellite, from, hash, expiresAt)
}

func (s *Store) FindByHash(_ context.Context, hash [sha256.Size]byte) (fleet.SatelliteAuth, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.record("FindByHash", hash); err != nil {
		return fleet.SatelliteAuth{}, err
	}
	auth, ok := s.Auth[hash]
	if !ok {
		return fleet.SatelliteAuth{}, fleet.ErrNotFound
	}
	return auth, nil
}

func (s *Store) FindByName(_ context.Context, satelliteName string, now time.Time) (fleet.SatelliteAuth, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.record("FindByName", satelliteName, now); err != nil {
		return fleet.SatelliteAuth{}, err
	}
	auth, ok := s.ByName[satelliteName]
	if !ok {
		return fleet.SatelliteAuth{}, fleet.ErrNotFound
	}
	return auth, nil
}

func (s *Store) DeleteForSatellite(_ context.Context, satellite fleet.SatelliteID) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.record("DeleteForSatellite", satellite)
}

func (s *Store) DeleteExpired(_ context.Context, now time.Time) (int64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.record("DeleteExpired", now); err != nil {
		return 0, err
	}
	return s.Expired, nil
}
