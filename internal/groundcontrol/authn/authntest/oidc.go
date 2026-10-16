package authntest

import (
	"context"
	"sync"

	"github.com/container-registry/harbor-satellite/internal/groundcontrol/authn"
	"github.com/container-registry/harbor-satellite/internal/groundcontrol/fleet"
)

var _ authn.OIDCProvider = (*Provider)(nil)

// Provider is a recording authn.OIDCProvider with settable answers. Its
// calls are recorded by method name with their arguments.
type Provider struct {
	mu    sync.Mutex
	calls []Call
	fails map[string]error
	// Answer answers Exchange, PollDeviceToken and VerifyIDToken.
	Answer authn.Claims
	Device fleet.DeviceAuthorization
	// PollAnswers are returned by PollDeviceToken in order; once used up it
	// answers Answer.
	PollAnswers []error
}

func (p *Provider) record(method string, args ...any) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.calls = append(p.calls, Call{Method: method, Args: args})
	return p.fails[method]
}

func (p *Provider) FailOn(method string, err error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.fails == nil {
		p.fails = map[string]error{}
	}
	p.fails[method] = err
}

func (p *Provider) Calls() []Call {
	p.mu.Lock()
	defer p.mu.Unlock()
	return append([]Call(nil), p.calls...)
}

func (p *Provider) AuthCodeURL(state, codeChallenge, nonce string) (string, error) {
	if err := p.record("AuthCodeURL", state, codeChallenge, nonce); err != nil {
		return "", err
	}
	return "https://idp.example/authorize?state=" + state, nil
}

func (p *Provider) Exchange(_ context.Context, code, codeVerifier, nonce string) (authn.Claims, error) {
	if err := p.record("Exchange", code, codeVerifier, nonce); err != nil {
		return authn.Claims{}, err
	}
	return p.Answer, nil
}

func (p *Provider) DeviceAuthorize(context.Context) (fleet.DeviceAuthorization, error) {
	if err := p.record("DeviceAuthorize"); err != nil {
		return fleet.DeviceAuthorization{}, err
	}
	return p.Device, nil
}

func (p *Provider) PollDeviceToken(_ context.Context, deviceCode string) (authn.Claims, error) {
	if err := p.record("PollDeviceToken", deviceCode); err != nil {
		return authn.Claims{}, err
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if len(p.PollAnswers) > 0 {
		err := p.PollAnswers[0]
		p.PollAnswers = p.PollAnswers[1:]
		return authn.Claims{}, err
	}
	return p.Answer, nil
}

func (p *Provider) VerifyIDToken(_ context.Context, rawIDToken string) (authn.Claims, error) {
	if err := p.record("VerifyIDToken", rawIDToken); err != nil {
		return authn.Claims{}, err
	}
	return p.Answer, nil
}
