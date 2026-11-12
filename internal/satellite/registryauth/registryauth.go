// Package registryauth provides the credential the satellite pulls from
// Harbor with: the robot today, a token exchanged for a JWT-SVID with
// keyless pull (plan item 8.3) once Harbor 3 offers the exchange.
package registryauth

import (
	"context"
	"time"

	"errors"
	"fmt"
	"sync"
)

// Credential is either Username and Password or a BearerToken.
type Credential struct {
	Username, Password, BearerToken string
	ExpiresAt                       time.Time
}

type Provider interface {
	Credential(ctx context.Context, registryHost string) (Credential, error)
}

type JWTSVIDSource interface {
	FetchJWTSVID(ctx context.Context, audience string) (string, error)
}

type TokenExchanger interface {
	Exchange(ctx context.Context, jwtSVID, audience string) (Credential, error)
}

// StaticProvider answers the robot credentials current at each call, so a
// rotation applies without a new provider.
type StaticProvider struct {
	Credentials func() (username, password string)
}

var _ Provider = StaticProvider{}

func (p StaticProvider) Credential(_ context.Context, _ string) (Credential, error) {
	username, password := p.Credentials()
	return Credential{Username: username, Password: password}, nil
}

// refreshBefore renews a pull token this long before it expires, so a
// pull that starts with it does not outlive it.
const refreshBefore = 30 * time.Second

// WorkloadIdentityProvider exchanges a JWT-SVID for a pull token and
// caches it until shortly before it expires. Harbor 3's workload identity
// federation is not released, so no TokenExchanger for it exists yet and
// cmd/satellite still pulls with the robot (docs/guides/keyless-pulls.md).
type WorkloadIdentityProvider struct {
	source    JWTSVIDSource
	exchanger TokenExchanger
	audience  string
	clock     func() time.Time
	mu        sync.Mutex
	cached    map[string]Credential
}

var _ Provider = (*WorkloadIdentityProvider)(nil)

func NewWorkloadIdentityProvider(source JWTSVIDSource, exchanger TokenExchanger, audience string, clock func() time.Time) (*WorkloadIdentityProvider, error) {
	if source == nil || exchanger == nil {
		return nil, errors.New("workload identity provider needs a JWT-SVID source and a token exchanger")
	}
	if audience == "" {
		return nil, errors.New("workload identity provider needs the registry audience")
	}
	if clock == nil {
		clock = time.Now
	}
	return &WorkloadIdentityProvider{source: source, exchanger: exchanger, audience: audience, clock: clock, cached: map[string]Credential{}}, nil
}

func (p *WorkloadIdentityProvider) Credential(ctx context.Context, registryHost string) (Credential, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if c, ok := p.cached[registryHost]; ok && p.clock().Before(c.ExpiresAt.Add(-refreshBefore)) {
		return c, nil
	}
	svid, err := p.source.FetchJWTSVID(ctx, p.audience)
	if err != nil {
		return Credential{}, fmt.Errorf("fetch JWT-SVID for %s: %w", p.audience, err)
	}
	c, err := p.exchanger.Exchange(ctx, svid, p.audience)
	if err != nil {
		return Credential{}, fmt.Errorf("exchange JWT-SVID for a pull token of %s: %w", registryHost, err)
	}
	if c.BearerToken == "" {
		return Credential{}, fmt.Errorf("token exchange for %s returned no bearer token", registryHost)
	}
	p.cached[registryHost] = c
	return c, nil
}
