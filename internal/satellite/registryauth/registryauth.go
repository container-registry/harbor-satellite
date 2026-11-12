// Package registryauth provides the credential the satellite pulls from
// Harbor with: the robot today, a token exchanged for a JWT-SVID with
// keyless pull (plan item 8.3, ports and fakes only).
package registryauth

import (
	"context"
	"time"

	"github.com/container-registry/harbor-satellite/internal/shared/notimpl"
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

// WorkloadIdentityProvider exchanges a JWT-SVID for a pull token and
// caches it until shortly before it expires.
type WorkloadIdentityProvider struct{}

var _ Provider = (*WorkloadIdentityProvider)(nil)

func NewWorkloadIdentityProvider(source JWTSVIDSource, exchanger TokenExchanger, audience string, clock func() time.Time) (*WorkloadIdentityProvider, error) {
	return nil, notimpl.Err("8.3 workload identity provider")
}

func (p *WorkloadIdentityProvider) Credential(ctx context.Context, registryHost string) (Credential, error) {
	return Credential{}, notimpl.Err("8.3 workload identity credential")
}
