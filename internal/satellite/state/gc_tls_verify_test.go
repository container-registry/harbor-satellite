package state

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/container-registry/harbor-satellite/internal/spiffe"
	"github.com/container-registry/harbor-satellite/pkg/config"
	"github.com/stretchr/testify/require"
)

// handlerRecord captures what a test server's handler saw, for the test
// goroutine to assert on once the request is done.
//
// Handlers must not assert directly: testify's require calls t.FailNow, which
// the testing package only permits from the goroutine running the test, so a
// failed assertion on a handler goroutine leaves the test in an undefined
// state. The mutex also keeps these fields from being written and read across
// goroutines unsynchronised.
type handlerRecord struct {
	mu       sync.Mutex
	called   bool
	writeErr error
}

func (r *handlerRecord) observe(writeErr error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.called = true
	r.writeErr = writeErr
}

func (r *handlerRecord) result() (called bool, writeErr error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	return r.called, r.writeErr
}

// tlsClientConfig digs the effective TLS settings out of the client's transport.
func tlsClientConfig(t *testing.T, client *http.Client) *tls.Config {
	t.Helper()
	transport, ok := client.Transport.(*http.Transport)
	require.True(t, ok, "expected *http.Transport")
	return transport.TLSClientConfig
}

// TestCreateHTTPClient_DefaultClientKeepsVerification checks the baseline: with
// skipTLSVerify off and no TLS material configured, the Ground Control client
// verifies server certificates. The use_unsecure coupling is covered end to end
// by TestRegisterSatellite_UseUnsecureStillVerifiesCert below.
func TestCreateHTTPClient_DefaultClientKeepsVerification(t *testing.T) {
	client, err := createHTTPClient(testContext(), config.TLSConfig{}, false)
	require.NoError(t, err)

	cfg := tlsClientConfig(t, client)
	if cfg != nil {
		require.False(t, cfg.InsecureSkipVerify,
			"the default Ground Control client must verify server certificates")
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

// writeTestCert generates a self-signed cert/key pair on disk and returns their
// paths, for exercising the custom-TLS branch of createHTTPClient.
func writeTestCert(t *testing.T) (certPath, keyPath string) {
	t.Helper()

	priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)

	template := x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: "satellite-client"},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(time.Hour),
		KeyUsage:              x509.KeyUsageKeyEncipherment | x509.KeyUsageDigitalSignature,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth, x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
		IsCA:                  true,
	}

	certDER, err := x509.CreateCertificate(rand.Reader, &template, &template, &priv.PublicKey, priv)
	require.NoError(t, err)
	keyDER, err := x509.MarshalECPrivateKey(priv)
	require.NoError(t, err)

	dir := t.TempDir()
	certPath = filepath.Join(dir, "cert.pem")
	keyPath = filepath.Join(dir, "key.pem")

	require.NoError(t, os.WriteFile(certPath,
		pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: certDER}), 0o600))
	require.NoError(t, os.WriteFile(keyPath,
		pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER}), 0o600))

	return certPath, keyPath
}

// TestCreateHTTPClient_TLSSkipVerifyCannotBypass is the regression test for the
// second bypass: tls.skip_verify governs registry connections, and must not be
// a back door for disabling Ground Control verification. With the dedicated
// setting off, verification stays on no matter what tls.skip_verify says.
func TestCreateHTTPClient_TLSSkipVerifyCannotBypass(t *testing.T) {
	certPath, keyPath := writeTestCert(t)

	client, err := createHTTPClient(testContext(), config.TLSConfig{
		CertFile:   certPath,
		KeyFile:    keyPath,
		CAFile:     certPath,
		SkipVerify: true, // must be ignored for the Ground Control channel
	}, false)
	require.NoError(t, err)

	cfg := tlsClientConfig(t, client)
	require.NotNil(t, cfg)
	require.False(t, cfg.InsecureSkipVerify,
		"tls.skip_verify must not disable Ground Control certificate verification")
}

// TestCreateHTTPClient_SkipVerifyKeepsClientCert covers the mTLS case: the skip
// flag relaxes *server* verification only. Dropping the client certificate
// alongside it would break any Ground Control that requires client auth, which
// is the opposite of what the flag is for.
func TestCreateHTTPClient_SkipVerifyKeepsClientCert(t *testing.T) {
	certPath, keyPath := writeTestCert(t)

	client, err := createHTTPClient(testContext(), config.TLSConfig{
		CertFile: certPath,
		KeyFile:  keyPath,
		CAFile:   certPath,
	}, true)
	require.NoError(t, err)

	cfg := tlsClientConfig(t, client)
	require.NotNil(t, cfg)
	require.True(t, cfg.InsecureSkipVerify, "skip flag must still relax server verification")
	require.Len(t, cfg.Certificates, 1, "client certificate must still be presented for mTLS")
	require.NotNil(t, cfg.RootCAs, "configured CA pool must still be loaded")
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
	var rec handlerRecord
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, werr := w.Write([]byte(`{}`))
		rec.observe(werr)
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

	called, writeErr := rec.result()
	require.NoError(t, writeErr)
	require.False(t, called, "registration token must not reach an unverified server")
}

// TestSendStatusReport_RequiresHTTPSEvenWithUseUnsecure is the regression test
// for the reporting path. The non-SPIFFE branch attaches registry Basic Auth
// credentials to the status report, so a plain-HTTP sync URL would put them on
// the wire in the clear. use_unsecure used to permit exactly that; it must no
// longer have any effect on this channel.
func TestSendStatusReport_RequiresHTTPSEvenWithUseUnsecure(t *testing.T) {
	var rec handlerRecord
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		rec.observe(nil)
	}))
	defer srv.Close()
	require.True(t, strings.HasPrefix(srv.URL, "http://"), "test server must be plain HTTP")

	cm, err := config.NewConfigManager("", "", "", "https://example.com", false, &config.Config{
		AppConfig: config.AppConfig{UseUnsecure: true},
	})
	require.NoError(t, err)

	s := NewStatusReportingProcess(cm)

	err = s.sendStatusReport(testContext(), srv.URL, &StatusReportParams{})

	require.Error(t, err, "a plain-HTTP sync URL must be refused even with use_unsecure")
	require.Contains(t, err.Error(), "must use HTTPS")

	called, writeErr := rec.result()
	require.NoError(t, writeErr)
	require.False(t, called, "credentials must not be sent over plain HTTP")
}

// TestReloadConfig_PreservesGCSkipTLSVerifyOverride covers the reconciliation
// bug: Ground Control ships config that replaces the local one wholesale, so a
// satellite started with --gc-skip-tls-verify would silently have verification
// turned back on mid-run and start failing against the certificate it was told
// to trust.
func TestReloadConfig_PreservesGCSkipTLSVerifyOverride(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "config.json")

	// Config as delivered by Ground Control: the field is absent/false.
	remote := `{"app_config":{"ground_control_url":"https://example.com","log_level":"info"}}`
	require.NoError(t, os.WriteFile(configPath, []byte(remote), 0o600))

	cm, _, err := config.InitConfigManager(
		"token", "https://example.com", configPath, filepath.Join(dir, "prev.json"),
		false, false, true, // gcSkipTLSVerify set locally via --gc-skip-tls-verify
	)
	require.NoError(t, err)
	require.True(t, cm.GroundControlSkipTLSVerify(), "local override must apply at startup")

	// Ground Control pushes a fresh config and the satellite reloads it.
	require.NoError(t, os.WriteFile(configPath, []byte(remote), 0o600))
	_, _, err = cm.ReloadConfig()
	require.NoError(t, err)

	require.True(t, cm.GroundControlSkipTLSVerify(),
		"local --gc-skip-tls-verify must survive config reconciliation")
}

// TestRegisterSatellite_SkipVerifyReachesServer confirms the dedicated opt-in
// still allows the self-signed server through, so the escape hatch works.
func TestRegisterSatellite_SkipVerifyReachesServer(t *testing.T) {
	var rec handlerRecord
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, werr := w.Write([]byte(`{}`))
		rec.observe(werr)
	}))
	defer srv.Close()

	_, err := registerSatellite(srv.URL, "register", "secret-token", config.TLSConfig{}, true, testContext())
	require.NoError(t, err)

	called, writeErr := rec.result()
	require.NoError(t, writeErr)
	require.True(t, called)
}

// TestTraceMatrix_OnlyGCFlagDisablesVerification enumerates the four
// combinations the reviewers asked to be traced and asserts that only
// ground_control_skip_tls_verify can ever produce InsecureSkipVerify=true on
// the Ground Control client.
func TestTraceMatrix_OnlyGCFlagDisablesVerification(t *testing.T) {
	certPath, keyPath := writeTestCert(t)

	cases := []struct {
		name          string
		useUnsecure   bool
		gcSkip        bool
		tlsSkipVerify bool
		withCerts     bool
		wantInsecure  bool
	}{
		{name: "use-unsecure alone", useUnsecure: true, wantInsecure: false},
		{name: "gc-skip-tls-verify alone", gcSkip: true, wantInsecure: true},
		{name: "both together", useUnsecure: true, gcSkip: true, wantInsecure: true},
		{name: "tls.skip_verify set", tlsSkipVerify: true, withCerts: true, wantInsecure: false},
		{name: "tls.skip_verify + use-unsecure", useUnsecure: true, tlsSkipVerify: true, withCerts: true, wantInsecure: false},
		{name: "tls.skip_verify + gc-skip", gcSkip: true, tlsSkipVerify: true, withCerts: true, wantInsecure: true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cm, err := config.NewConfigManager("", "", "", "https://example.com", false, &config.Config{
				AppConfig: config.AppConfig{
					UseUnsecure:                tc.useUnsecure,
					GroundControlSkipTLSVerify: tc.gcSkip,
				},
			})
			require.NoError(t, err)

			tlsCfg := config.TLSConfig{SkipVerify: tc.tlsSkipVerify}
			if tc.withCerts {
				tlsCfg.CertFile, tlsCfg.KeyFile, tlsCfg.CAFile = certPath, keyPath, certPath
			}

			client, err := createHTTPClient(testContext(), tlsCfg, cm.GroundControlSkipTLSVerify())
			require.NoError(t, err)

			got := tlsClientConfig(t, client)
			require.NotNil(t, got)
			require.Equal(t, tc.wantInsecure, got.InsecureSkipVerify,
				"use_unsecure=%v gc_skip=%v tls.skip_verify=%v", tc.useUnsecure, tc.gcSkip, tc.tlsSkipVerify)
		})
	}
}

// TestSendStatusReport_SPIFFEAlsoRequiresHTTPS covers the SPIFFE path. The
// HTTPS check used to sit inside the non-SPIFFE branch, so a SPIFFE-enabled
// satellite pointed at an http:// Ground Control sent status reports in
// plaintext: a transport's TLS config is never applied to an http:// URL, so
// holding an SVID does not make the connection encrypted.
//
// A non-nil spiffeClient here would fail on Connect if it were reached; getting
// the HTTPS error instead proves the check runs before the client split.
func TestSendStatusReport_SPIFFEAlsoRequiresHTTPS(t *testing.T) {
	cm, err := config.NewConfigManager("", "", "", "https://example.com", false, &config.Config{
		AppConfig: config.AppConfig{},
	})
	require.NoError(t, err)

	s := NewStatusReportingProcess(cm)
	s.spiffeClient = &spiffe.Client{}

	err = s.sendStatusReport(testContext(), "http://ground-control.example.com", &StatusReportParams{})

	require.Error(t, err)
	require.Contains(t, err.Error(), "must use HTTPS",
		"the SPIFFE path must reject a plain-HTTP sync URL, not attempt to send")
}

// TestCreateHTTPClient_NegotiatesHTTP2 guards the protocol regression: this
// transport always carries a TLS config, and net/http disables automatic HTTP/2
// whenever TLSClientConfig is non-nil unless ForceAttemptHTTP2 is set. Before
// this PR the default path left TLSClientConfig nil and so spoke HTTP/2.
func TestCreateHTTPClient_NegotiatesHTTP2(t *testing.T) {
	var rec handlerRecord
	srv := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, werr := w.Write([]byte(r.Proto))
		rec.observe(werr)
	}))
	srv.EnableHTTP2 = true
	srv.StartTLS()
	defer srv.Close()

	// skipTLSVerify lets the client accept the test server's self-signed cert.
	client, err := createHTTPClient(testContext(), config.TLSConfig{}, true)
	require.NoError(t, err)

	resp, err := client.Get(srv.URL)
	require.NoError(t, err)
	defer func() { require.NoError(t, resp.Body.Close()) }()

	_, writeErr := rec.result()
	require.NoError(t, writeErr)

	require.Equal(t, "HTTP/2.0", resp.Proto,
		"Ground Control connections must not silently drop to HTTP/1.1")
}
