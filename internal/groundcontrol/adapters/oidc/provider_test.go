package oidc_test

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync"
	"testing"
	"time"

	"github.com/container-registry/harbor-satellite/internal/groundcontrol/adapters/oidc"
	"github.com/container-registry/harbor-satellite/internal/groundcontrol/authn"
	"github.com/container-registry/harbor-satellite/internal/groundcontrol/fleet"
	"github.com/coreos/go-oidc/v3/oidc/oidctest"
	"github.com/stretchr/testify/require"
)

// idp is an OIDC provider with code and device flows that signs ID tokens
// with a key it advertises, as Dex does.
type idp struct {
	t       *testing.T
	srv     *httptest.Server
	key     *rsa.PrivateKey
	keys    *oidctest.Server
	mu      sync.Mutex
	nonce   string
	polls   int
	pending int
	form    url.Values
}

func newIDP(t *testing.T) *idp {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)
	p := &idp{t: t, key: key, keys: &oidctest.Server{PublicKeys: []oidctest.PublicKey{{PublicKey: key.Public(), KeyID: "k1", Algorithm: "RS256"}}}}
	mux := http.NewServeMux()
	mux.HandleFunc("/.well-known/openid-configuration", func(w http.ResponseWriter, _ *http.Request) {
		require.NoError(t, json.NewEncoder(w).Encode(map[string]any{
			"issuer": p.srv.URL, "authorization_endpoint": p.srv.URL + "/auth", "token_endpoint": p.srv.URL + "/token",
			"jwks_uri": p.srv.URL + "/keys", "device_authorization_endpoint": p.srv.URL + "/device",
			"response_types_supported": []string{"code"}, "subject_types_supported": []string{"public"},
			"id_token_signing_alg_values_supported": []string{"RS256"},
		}))
	})
	mux.Handle("/keys", p.keys)
	mux.HandleFunc("/device", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		require.NoError(t, json.NewEncoder(w).Encode(map[string]any{
			"device_code": "dev-1", "user_code": "ABCD-EFGH", "verification_uri": p.srv.URL + "/activate", "expires_in": 600, "interval": 5,
		}))
	})
	mux.HandleFunc("/token", func(w http.ResponseWriter, r *http.Request) {
		require.NoError(t, r.ParseForm())
		p.mu.Lock()
		defer p.mu.Unlock()
		p.form = r.PostForm
		w.Header().Set("Content-Type", "application/json")
		if r.PostForm.Get("grant_type") == "urn:ietf:params:oauth:grant-type:device_code" {
			p.polls++
			if p.polls <= p.pending {
				w.WriteHeader(http.StatusBadRequest)
				require.NoError(t, json.NewEncoder(w).Encode(map[string]string{"error": "authorization_pending"}))
				return
			}
		}
		require.NoError(t, json.NewEncoder(w).Encode(map[string]any{"access_token": "at", "token_type": "Bearer", "id_token": p.idToken("gc", p.nonce)}))
	})
	p.srv = httptest.NewServer(mux)
	p.keys.SetIssuer(p.srv.URL)
	t.Cleanup(p.srv.Close)
	return p
}

func (p *idp) idToken(audience, nonce string) string {
	claims := fmt.Sprintf(`{"iss":%q,"sub":"sub-alice","aud":%q,"exp":%d,"iat":%d,"nonce":%q,"preferred_username":"alice","email":"alice@example.com","groups":["edge-ops","other"]}`,
		p.srv.URL, audience, time.Now().Add(time.Hour).Unix(), time.Now().Unix(), nonce)
	return oidctest.SignIDToken(p.key, "k1", "RS256", claims)
}

func provider(t *testing.T, p *idp) *oidc.Provider {
	t.Helper()
	prov, err := oidc.New(context.Background(), authn.OIDCConfig{
		IssuerURL: p.srv.URL, ClientID: "gc", ClientSecret: "secret", RedirectURL: "https://gc.example/oidc/callback",
		Scopes: []string{"openid", "groups"}, UsernameClaim: "preferred_username", GroupsClaim: "groups",
	})
	require.NoError(t, err)
	return prov
}

func TestProvider_CodeFlowWithPKCEAndNonce(t *testing.T) {
	p := newIDP(t)
	prov := provider(t, p)

	raw, err := prov.AuthCodeURL("state-1", "challenge-1", "nonce-1")
	require.NoError(t, err)
	u, err := url.Parse(raw)
	require.NoError(t, err)
	require.Equal(t, "challenge-1", u.Query().Get("code_challenge"))
	require.Equal(t, "S256", u.Query().Get("code_challenge_method"))
	require.Equal(t, "nonce-1", u.Query().Get("nonce"))
	require.Equal(t, "state-1", u.Query().Get("state"))

	p.nonce = "nonce-1"
	claims, err := prov.Exchange(context.Background(), "code-1", "verifier-1", "nonce-1")
	require.NoError(t, err)
	require.Equal(t, "verifier-1", p.form.Get("code_verifier"), "the PKCE verifier reaches the token endpoint")
	require.Equal(t, authn.Claims{Issuer: p.srv.URL, Subject: "sub-alice", Username: "alice", Email: "alice@example.com", Groups: []string{"edge-ops", "other"}}, claims)

	_, err = prov.Exchange(context.Background(), "code-1", "verifier-1", "another-nonce")
	require.ErrorContains(t, err, "nonce", "a replayed ID token of another login is refused")
}

func TestProvider_RejectsTokenForAnotherAudience(t *testing.T) {
	p := newIDP(t)
	prov := provider(t, p)

	_, err := prov.VerifyIDToken(context.Background(), p.idToken("another-client", ""))

	require.Error(t, err)
}

func TestProvider_DeviceFlowPollsOnce(t *testing.T) {
	p := newIDP(t)
	p.pending = 1
	prov := provider(t, p)

	auth, err := prov.DeviceAuthorize(context.Background())
	require.NoError(t, err)
	require.Equal(t, "dev-1", auth.DeviceCode)
	require.Equal(t, 5*time.Second, auth.Interval)

	_, err = prov.PollDeviceToken(context.Background(), "dev-1")
	require.ErrorIs(t, err, fleet.ErrAuthorizationPending)
	claims, err := prov.PollDeviceToken(context.Background(), "dev-1")
	require.NoError(t, err)
	require.Equal(t, "alice", claims.Username)
	require.Equal(t, 2, p.polls, "one token request per poll")
}
