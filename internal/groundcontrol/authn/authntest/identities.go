package authntest

import (
	"context"
	"crypto/sha256"
	"sync"
	"time"

	"github.com/container-registry/harbor-satellite/internal/groundcontrol/authn"
	"github.com/container-registry/harbor-satellite/internal/groundcontrol/fleet"
)

var _ authn.IdentityStore = (*Identities)(nil)

// Identities is an in-memory authn.IdentityStore with the SQL semantics:
// LocalUsers are local accounts an identity never adopts, and a login
// state is taken once with its binding before it expires.
type Identities struct {
	mu         sync.Mutex
	calls      []Call
	users      map[string]int32 // issuer|subject → user ID
	states     map[string]authn.LoginState
	nextID     int32
	LocalUsers map[string]bool
}

func NewIdentities() *Identities {
	return &Identities{users: map[string]int32{}, states: map[string]authn.LoginState{}, LocalUsers: map[string]bool{}, nextID: 100}
}

func (s *Identities) Calls() []Call {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]Call(nil), s.calls...)
}

func (s *Identities) Count(method string) int {
	n := 0
	for _, c := range s.Calls() {
		if c.Method == method {
			n++
		}
	}
	return n
}

// States returns the stored login states.
func (s *Identities) States() []authn.LoginState {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]authn.LoginState, 0, len(s.states))
	for _, st := range s.states {
		out = append(out, st)
	}
	return out
}

func (s *Identities) Upsert(_ context.Context, claims authn.Claims, role string) (int32, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.calls = append(s.calls, Call{Method: "Upsert", Args: []any{claims, role}})
	key := claims.Issuer + "|" + claims.Subject
	if id, ok := s.users[key]; ok {
		return id, nil
	}
	if s.LocalUsers[claims.Username] {
		return 0, fleet.ErrForbidden
	}
	s.nextID++
	s.users[key] = s.nextID
	return s.nextID, nil
}

func (s *Identities) SaveLoginState(_ context.Context, state authn.LoginState) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.calls = append(s.calls, Call{Method: "SaveLoginState", Args: []any{state}})
	s.states[state.State] = state
	return nil
}

func (s *Identities) TakeLoginState(_ context.Context, state string, bindingHash [sha256.Size]byte, now time.Time) (string, string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.calls = append(s.calls, Call{Method: "TakeLoginState", Args: []any{state, bindingHash, now}})
	st, ok := s.states[state]
	if !ok || st.BindingHash != bindingHash || !now.Before(st.ExpiresAt) {
		return "", "", fleet.ErrNotFound
	}
	delete(s.states, state)
	return st.CodeVerifier, st.Nonce, nil
}
