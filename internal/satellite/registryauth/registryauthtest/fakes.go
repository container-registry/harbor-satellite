// Package registryauthtest has recording fakes of the registryauth ports.
package registryauthtest

import (
	"context"
	"sync"

	"github.com/container-registry/harbor-satellite/internal/satellite/registryauth"
)

// Calls records calls per method and fails the methods set with FailOn.
type Calls struct {
	mu     sync.Mutex
	counts map[string]int
	fails  map[string]error
}

func (c *Calls) record(method string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.counts == nil {
		c.counts = map[string]int{}
	}
	c.counts[method]++
	return c.fails[method]
}

func (c *Calls) FailOn(method string, err error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.fails == nil {
		c.fails = map[string]error{}
	}
	c.fails[method] = err
}

func (c *Calls) Count(method string) int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.counts[method]
}

var (
	_ registryauth.JWTSVIDSource  = (*JWTSVIDSource)(nil)
	_ registryauth.TokenExchanger = (*TokenExchanger)(nil)
)

// JWTSVIDSource answers SVID; its calls are recorded as "FetchJWTSVID".
type JWTSVIDSource struct {
	Calls
	SVID string
}

func (s *JWTSVIDSource) FetchJWTSVID(_ context.Context, _ string) (string, error) {
	if err := s.record("FetchJWTSVID"); err != nil {
		return "", err
	}
	return s.SVID, nil
}

// TokenExchanger answers Credential; its calls are recorded as "Exchange".
type TokenExchanger struct {
	Calls
	Credential registryauth.Credential
}

func (e *TokenExchanger) Exchange(_ context.Context, _, _ string) (registryauth.Credential, error) {
	if err := e.record("Exchange"); err != nil {
		return registryauth.Credential{}, err
	}
	return e.Credential, nil
}
