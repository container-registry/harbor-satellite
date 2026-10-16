// Package oidc is the OIDC provider adapter of plan item 8.1. It has no
// go-oidc dependency until the implementation lands.
package oidc

import (
	"context"

	"github.com/container-registry/harbor-satellite/internal/groundcontrol/authn"
	"github.com/container-registry/harbor-satellite/internal/groundcontrol/fleet"
	"github.com/container-registry/harbor-satellite/internal/shared/notimpl"
)

type Provider struct{}

var _ authn.OIDCProvider = (*Provider)(nil)

func New(ctx context.Context, cfg authn.OIDCConfig) (*Provider, error) {
	return nil, notimpl.Err("8.1 OIDC provider")
}

func (p *Provider) AuthCodeURL(state, codeChallenge, nonce string) (string, error) {
	return "", notimpl.Err("8.1 OIDC auth code URL")
}

func (p *Provider) Exchange(ctx context.Context, code, codeVerifier, nonce string) (authn.Claims, error) {
	return authn.Claims{}, notimpl.Err("8.1 OIDC exchange")
}

func (p *Provider) DeviceAuthorize(ctx context.Context) (fleet.DeviceAuthorization, error) {
	return fleet.DeviceAuthorization{}, notimpl.Err("8.1 OIDC device authorization")
}

func (p *Provider) PollDeviceToken(ctx context.Context, deviceCode string) (authn.Claims, error) {
	return authn.Claims{}, notimpl.Err("8.1 OIDC device token")
}

func (p *Provider) VerifyIDToken(ctx context.Context, rawIDToken string) (authn.Claims, error) {
	return authn.Claims{}, notimpl.Err("8.1 OIDC verify ID token")
}
