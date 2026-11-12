// Package oidc is the OIDC provider adapter of plan item 8.1 (ADR-0018):
// coreos/go-oidc for discovery and ID token verification, x/oauth2 for the
// authorization code flow with PKCE, and a single-poll device token
// request, because Ground Control answers each CLI poll on its own.
package oidc

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/container-registry/harbor-satellite/internal/groundcontrol/authn"
	"github.com/container-registry/harbor-satellite/internal/groundcontrol/fleet"
	gooidc "github.com/coreos/go-oidc/v3/oidc"
	"golang.org/x/oauth2"
)

type Provider struct {
	cfg      authn.OIDCConfig
	oauth    oauth2.Config
	verifier *gooidc.IDTokenVerifier
	client   *http.Client
}

var _ authn.OIDCProvider = (*Provider)(nil)

// New discovers the provider at cfg.IssuerURL. ctx may carry an
// *http.Client under oauth2.HTTPClient, as tests use it.
func New(ctx context.Context, cfg authn.OIDCConfig) (*Provider, error) {
	if cfg.IssuerURL == "" || cfg.ClientID == "" {
		return nil, fmt.Errorf("%w: OIDC needs OIDC_ISSUER_URL and OIDC_CLIENT_ID", fleet.ErrInvalid)
	}
	p, err := gooidc.NewProvider(ctx, cfg.IssuerURL)
	if err != nil {
		return nil, fmt.Errorf("discover OIDC provider %s: %w", cfg.IssuerURL, err)
	}
	scopes := cfg.Scopes
	if len(scopes) == 0 {
		scopes = []string{gooidc.ScopeOpenID}
	}
	client := http.DefaultClient
	if c, ok := ctx.Value(oauth2.HTTPClient).(*http.Client); ok && c != nil {
		client = c
	}
	return &Provider{
		cfg: cfg,
		oauth: oauth2.Config{
			ClientID: cfg.ClientID, ClientSecret: cfg.ClientSecret, RedirectURL: cfg.RedirectURL,
			Endpoint: p.Endpoint(), Scopes: scopes,
		},
		verifier: p.Verifier(&gooidc.Config{ClientID: cfg.ClientID}),
		client:   client,
	}, nil
}

func (p *Provider) ctx(ctx context.Context) context.Context {
	return context.WithValue(ctx, oauth2.HTTPClient, p.client)
}

func (p *Provider) AuthCodeURL(state, codeChallenge, nonce string) (string, error) {
	return p.oauth.AuthCodeURL(state,
		oauth2.SetAuthURLParam("code_challenge", codeChallenge),
		oauth2.SetAuthURLParam("code_challenge_method", "S256"),
		gooidc.Nonce(nonce),
	), nil
}

func (p *Provider) Exchange(ctx context.Context, code, codeVerifier, nonce string) (authn.Claims, error) {
	tok, err := p.oauth.Exchange(p.ctx(ctx), code, oauth2.VerifierOption(codeVerifier))
	if err != nil {
		return authn.Claims{}, fmt.Errorf("exchange code: %w", err)
	}
	raw, ok := tok.Extra("id_token").(string)
	if !ok || raw == "" {
		return authn.Claims{}, errors.New("token response carries no id_token")
	}
	return p.verify(ctx, raw, nonce)
}

// VerifyIDToken checks signature, issuer, audience and expiry.
func (p *Provider) VerifyIDToken(ctx context.Context, rawIDToken string) (authn.Claims, error) {
	return p.verify(ctx, rawIDToken, "")
}

func (p *Provider) verify(ctx context.Context, raw, nonce string) (authn.Claims, error) {
	idToken, err := p.verifier.Verify(p.ctx(ctx), raw)
	if err != nil {
		return authn.Claims{}, fmt.Errorf("verify ID token: %w", err)
	}
	if nonce != "" && idToken.Nonce != nonce {
		return authn.Claims{}, errors.New("ID token nonce does not match the login")
	}
	var all map[string]any
	if err := idToken.Claims(&all); err != nil {
		return authn.Claims{}, fmt.Errorf("decode ID token claims: %w", err)
	}
	claims := authn.Claims{Issuer: idToken.Issuer, Subject: idToken.Subject}
	if username, ok := all[p.cfg.UsernameClaim].(string); ok {
		claims.Username = username
	}
	if email, ok := all["email"].(string); ok {
		claims.Email = email
	}
	switch groups := all[p.cfg.GroupsClaim].(type) {
	case []any:
		for _, g := range groups {
			if s, ok := g.(string); ok && s != "" {
				claims.Groups = append(claims.Groups, s)
			}
		}
	case string:
		claims.Groups = []string{groups}
	}
	return claims, nil
}

func (p *Provider) DeviceAuthorize(ctx context.Context) (fleet.DeviceAuthorization, error) {
	if p.oauth.Endpoint.DeviceAuthURL == "" {
		return fleet.DeviceAuthorization{}, fmt.Errorf("%w: the provider advertises no device_authorization_endpoint", fleet.ErrInvalid)
	}
	resp, err := p.oauth.DeviceAuth(p.ctx(ctx))
	if err != nil {
		return fleet.DeviceAuthorization{}, fmt.Errorf("device authorization: %w", err)
	}
	auth := fleet.DeviceAuthorization{
		DeviceCode: resp.DeviceCode, UserCode: resp.UserCode, VerificationURI: resp.VerificationURI,
		VerificationURIComplete: resp.VerificationURIComplete, Interval: time.Duration(resp.Interval) * time.Second,
	}
	if !resp.Expiry.IsZero() {
		auth.ExpiresIn = time.Until(resp.Expiry).Round(time.Second)
	}
	if auth.Interval <= 0 {
		auth.Interval = 5 * time.Second
	}
	return auth, nil
}

type tokenAnswer struct {
	IDToken string `json:"id_token"`
	Error   string `json:"error"`
	Desc    string `json:"error_description"`
}

// PollDeviceToken asks the token endpoint once (RFC 8628 3.4).
func (p *Provider) PollDeviceToken(ctx context.Context, deviceCode string) (authn.Claims, error) {
	form := url.Values{
		"grant_type":  {"urn:ietf:params:oauth:grant-type:device_code"},
		"device_code": {deviceCode},
		"client_id":   {p.oauth.ClientID},
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, p.oauth.Endpoint.TokenURL, strings.NewReader(form.Encode()))
	if err != nil {
		return authn.Claims{}, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")
	if p.oauth.ClientSecret != "" {
		req.SetBasicAuth(url.QueryEscape(p.oauth.ClientID), url.QueryEscape(p.oauth.ClientSecret))
	}
	resp, err := p.client.Do(req)
	if err != nil {
		return authn.Claims{}, fmt.Errorf("device token: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return authn.Claims{}, fmt.Errorf("device token: %w", err)
	}
	var answer tokenAnswer
	if err := json.Unmarshal(body, &answer); err != nil {
		return authn.Claims{}, fmt.Errorf("device token answer (%d): %w", resp.StatusCode, err)
	}
	switch answer.Error {
	case "":
	case "authorization_pending":
		return authn.Claims{}, fleet.ErrAuthorizationPending
	case "slow_down":
		return authn.Claims{}, fleet.ErrSlowDown
	default:
		return authn.Claims{}, fmt.Errorf("device token: %s %s", answer.Error, answer.Desc)
	}
	if answer.IDToken == "" {
		return authn.Claims{}, errors.New("device token answer carries no id_token")
	}
	return p.verify(ctx, answer.IDToken, "")
}
