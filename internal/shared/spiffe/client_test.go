//go:build !nospiffe

package spiffe

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/container-registry/harbor-satellite/internal/shared/spiffe/spiffetest"
	"github.com/spiffe/go-spiffe/v2/spiffetls/tlsconfig"
	"github.com/stretchr/testify/require"
)

const (
	td       = "harbor-satellite.test"
	satPath  = "/satellite/region/default/edge-01"
	gcPath   = "/gc/main"
	gcIDText = "spiffe://" + td + gcPath
)

func TestNewClient(t *testing.T) {
	_, err := NewClient(Config{Enabled: false})
	require.ErrorContains(t, err, "not enabled")

	_, err = NewClient(Config{Enabled: true, ExpectedServerID: "https://not-spiffe/gc"})
	require.ErrorContains(t, err, "invalid expected server ID")

	c, err := NewClient(Config{Enabled: true, EndpointSocket: DefaultConfig().EndpointSocket})
	require.NoError(t, err)
	require.True(t, c.expectedServerID.IsZero())
	require.False(t, DefaultConfig().Enabled)
}

func TestUnconnectedAndClosedClient(t *testing.T) {
	c, err := NewClient(Config{Enabled: true, EndpointSocket: "unix:///nonexistent.sock"})
	require.NoError(t, err)

	_, err = c.GetSVID()
	require.ErrorContains(t, err, "not connected")
	_, err = c.GetSPIFFEID()
	require.ErrorContains(t, err, "not connected")
	_, err = c.GetTLSConfig()
	require.ErrorContains(t, err, "not connected")
	_, err = c.CreateHTTPClient()
	require.ErrorContains(t, err, "not connected")

	require.NoError(t, c.Close())
	require.NoError(t, c.Close(), "a second close is a no-op")
	require.ErrorContains(t, c.Connect(context.Background()), "client is closed")
	_, err = c.GetSVID()
	require.ErrorContains(t, err, "client is closed")
	_, err = c.GetTLSConfig()
	require.ErrorContains(t, err, "client is closed")
}

func TestConnectFailsWhenTheAgentIsMissing(t *testing.T) {
	c, err := NewClient(Config{Enabled: true, EndpointSocket: "unix:///nonexistent/agent.sock"})
	require.NoError(t, err)
	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()
	require.ErrorContains(t, c.Connect(ctx), "create X509Source")
}

func TestWaitForSVIDTimesOut(t *testing.T) {
	c, err := NewClient(Config{Enabled: true})
	require.NoError(t, err)
	start := time.Now()
	require.ErrorContains(t, c.WaitForSVID(context.Background(), 100*time.Millisecond), "timeout waiting for SVID")
	require.Less(t, time.Since(start), 2*time.Second)
}

func connected(t *testing.T, ca *spiffetest.CA, expectedServerID string) *Client {
	t.Helper()
	agent := spiffetest.StartWorkloadAPI(t, ca, ca.SVID(t, satPath))
	c, err := NewClient(Config{Enabled: true, EndpointSocket: agent.Addr, ExpectedServerID: expectedServerID})
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, c.Close()) })
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	require.NoError(t, c.Connect(ctx))
	require.NoError(t, c.Connect(ctx), "connecting again keeps the source")
	return c
}

func TestConnectedClientServesItsSVID(t *testing.T) {
	ca := spiffetest.NewCA(t, td)
	c := connected(t, ca, "")

	require.NoError(t, c.WaitForSVID(context.Background(), 2*time.Second))
	id, err := c.GetSPIFFEID()
	require.NoError(t, err)
	require.Equal(t, "spiffe://"+td+satPath, id.String())
}

// gcServer is an mTLS server presenting the SVID at path and requiring a
// client SVID of the same trust domain, as Ground Control does.
func gcServer(t *testing.T, ca *spiffetest.CA, path string) *httptest.Server {
	t.Helper()
	svid := ca.SVID(t, path)
	srv := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id, err := ExtractSPIFFEIDFromCert(r.TLS.PeerCertificates[0])
		if err != nil {
			http.Error(w, err.Error(), http.StatusUnauthorized)
			return
		}
		if _, err := io.WriteString(w, id.String()); err != nil {
			t.Error(err)
		}
	}))
	// StartTLS would add its own certificate, which Go prefers to
	// GetCertificate for a client that sends no server name.
	srv.Listener = tls.NewListener(srv.Listener, tlsconfig.MTLSServerConfig(svid, ca.Bundle(), tlsconfig.AuthorizeMemberOf(ca.TrustDomain)))
	srv.Start()
	srv.URL = strings.Replace(srv.URL, "http://", "https://", 1)
	t.Cleanup(srv.Close)
	return srv
}

func TestHTTPClientDoesMutualTLS(t *testing.T) {
	ca := spiffetest.NewCA(t, td)
	srv := gcServer(t, ca, gcPath)

	tests := []struct {
		name     string
		expected string
		wantErr  string
	}{
		{name: "expected server ID", expected: gcIDText},
		{name: "any server of the bundle", expected: ""},
		{name: "another server ID", expected: "spiffe://" + td + "/gc/other", wantErr: "unexpected ID"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client, err := connected(t, ca, tt.expected).CreateHTTPClient()
			require.NoError(t, err)
			resp, err := client.Get(srv.URL)
			if tt.wantErr != "" {
				require.ErrorContains(t, err, tt.wantErr)
				return
			}
			require.NoError(t, err)
			defer resp.Body.Close()
			body, err := io.ReadAll(resp.Body)
			require.NoError(t, err)
			require.Equal(t, http.StatusOK, resp.StatusCode)
			require.Equal(t, "spiffe://"+td+satPath, string(body), "the server sees the satellite's SVID")
		})
	}
}

func TestHTTPClientRejectsAServerOfAnotherTrustDomain(t *testing.T) {
	ours := spiffetest.NewCA(t, td)
	theirs := spiffetest.NewCA(t, "other.test")
	srv := gcServer(t, theirs, gcPath)

	client, err := connected(t, ours, "").CreateHTTPClient()
	require.NoError(t, err)
	_, err = client.Get(srv.URL)
	require.ErrorContains(t, err, `no X.509 bundle for trust domain "other.test"`)
}

func TestExtractSPIFFEIDFromCert(t *testing.T) {
	spiffeURI, err := url.Parse("spiffe://" + td + satPath)
	require.NoError(t, err)
	httpsURI, err := url.Parse("https://example.test/x")
	require.NoError(t, err)
	badURI := &url.URL{Scheme: "spiffe", Host: "UPPER", Path: "/x"}

	tests := []struct {
		name string
		uris []*url.URL
		want string
		err  string
	}{
		{name: "no URI SANs", err: "no URI SANs"},
		{name: "no spiffe URI", uris: []*url.URL{httpsURI}, err: "no SPIFFE ID found"},
		{name: "spiffe URI after another", uris: []*url.URL{httpsURI, spiffeURI}, want: "spiffe://" + td + satPath},
		{name: "malformed spiffe URI", uris: []*url.URL{badURI}, err: "trust domain"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			id, err := ExtractSPIFFEIDFromCert(&x509.Certificate{URIs: tt.uris})
			if tt.err != "" {
				require.ErrorContains(t, err, tt.err)
				return
			}
			require.NoError(t, err)
			require.Equal(t, tt.want, id.String())
		})
	}
}
