package state

import (
	"crypto/tls"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/container-registry/harbor-satellite/pkg/config"
	"github.com/stretchr/testify/require"
)

// tlsClientConfig digs the effective TLS settings out of the client's transport.
func tlsClientConfig(t *testing.T, client *http.Client) *tls.Config {
	t.Helper()
	transport, ok := client.Transport.(*http.Transport)
	require.True(t, ok, "expected *http.Transport")
	return transport.TLSClientConfig
}

// TestCreateHTTPClient_UseUnsecureDoesNotSkipVerify is the regression test for
// the finding: --use-unsecure permits plain-HTTP registry connections and must
// not weaken Ground Control certificate verification. createHTTPClient no
// longer takes use_unsecure at all, so the guarantee is that the default
// (skipTLSVerify=false) leaves InsecureSkipVerify false regardless.
func TestCreateHTTPClient_UseUnsecureDoesNotSkipVerify(t *testing.T) {
	client, err := createHTTPClient(testContext(), config.TLSConfig{}, false)
	require.NoError(t, err)

	cfg := tlsClientConfig(t, client)
	if cfg != nil {
		require.False(t, cfg.InsecureSkipVerify,
			"Ground Control certificate verification must stay on when only use_unsecure is set")
	}
}

// TestCreateHTTPClient_SkipVerifyOptIn checks the dedicated setting still works
// when explicitly requested.
func TestCreateHTTPClient_SkipVerifyOptIn(t *testing.T) {
	client, err := createHTTPClient(testContext(), config.TLSConfig{}, true)
	require.NoError(t, err)

	cfg := tlsClientConfig(t, client)
	require.NotNil(t, cfg)
	require.True(t, cfg.InsecureSkipVerify)
	require.Equal(t, uint16(tls.VersionTLS12), cfg.MinVersion)
}

// TestGroundControlSkipTLSVerify_NotDerivedFromUseUnsecure asserts the two
// settings are independent at the config layer, so no future caller can
// reintroduce the coupling by reading the wrong getter.
func TestGroundControlSkipTLSVerify_NotDerivedFromUseUnsecure(t *testing.T) {
	cm, err := config.NewConfigManager("", "", "", "https://example.com", false, &config.Config{
		AppConfig: config.AppConfig{UseUnsecure: true},
	})
	require.NoError(t, err)

	require.True(t, cm.UseUnsecure())
	require.False(t, cm.GroundControlSkipTLSVerify(),
		"use_unsecure alone must not disable Ground Control certificate verification")
}

// TestRegisterSatellite_UseUnsecureStillVerifiesCert is the end-to-end
// regression test. It reproduces the production call site with use_unsecure
// enabled and points registration at a TLS server presenting an untrusted
// self-signed certificate — the impersonating Ground Control from the finding.
//
// Previously the call site passed cm.UseUnsecure() as the skip-verify flag, so
// this handshake succeeded and the registration token was handed to the
// attacker. It must now fail.
func TestRegisterSatellite_UseUnsecureStillVerifiesCert(t *testing.T) {
	var tokenReceived bool
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		tokenReceived = true
		w.WriteHeader(http.StatusOK)
		_, werr := w.Write([]byte(`{}`))
		require.NoError(t, werr)
	}))
	defer srv.Close()

	cm, err := config.NewConfigManager("", "", "", "https://example.com", false, &config.Config{
		AppConfig: config.AppConfig{UseUnsecure: true},
	})
	require.NoError(t, err)

	// Same arguments the ZtrProcess passes in production.
	_, err = registerSatellite(
		srv.URL,
		"register",
		"secret-token",
		cm.GetTLSConfig(),
		cm.GroundControlSkipTLSVerify(),
		testContext(),
	)

	require.Error(t, err, "handshake against an untrusted certificate must fail even with use_unsecure")
	require.False(t, tokenReceived, "registration token must not reach an unverified server")
}

// TestRegisterSatellite_SkipVerifyReachesServer confirms the dedicated opt-in
// still allows the self-signed server through, so the escape hatch works.
func TestRegisterSatellite_SkipVerifyReachesServer(t *testing.T) {
	var tokenReceived bool
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		tokenReceived = true
		w.WriteHeader(http.StatusOK)
		_, werr := w.Write([]byte(`{}`))
		require.NoError(t, werr)
	}))
	defer srv.Close()

	_, err := registerSatellite(srv.URL, "register", "secret-token", config.TLSConfig{}, true, testContext())
	require.NoError(t, err)
	require.True(t, tokenReceived)
}
