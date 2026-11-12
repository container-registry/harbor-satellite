//go:build !nospiffe

package spiffe

import (
	"context"
	"crypto/tls"
	"io"
	"net"
	"net/http"
	"path/filepath"
	"testing"
	"time"

	"github.com/container-registry/harbor-satellite/internal/shared/env"
	"github.com/container-registry/harbor-satellite/internal/shared/spiffe/spiffetest"
	"github.com/spiffe/go-spiffe/v2/spiffeid"
	"github.com/spiffe/go-spiffe/v2/spiffetls/tlsconfig"
	"github.com/spiffe/go-spiffe/v2/svid/x509svid"
	"github.com/stretchr/testify/require"
)

const gcPath = "/gc/main"

// serveTLS runs an HTTPS server with cfg that answers the client's SPIFFE ID.
func serveTLS(t *testing.T, cfg *tls.Config) string {
	t.Helper()
	lis, err := (&net.ListenConfig{}).Listen(context.Background(), "tcp", "127.0.0.1:0")
	require.NoError(t, err)
	srv := &http.Server{
		ReadHeaderTimeout: 5 * time.Second,
		Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			id, err := ExtractSPIFFEIDFromRequest(r)
			if err != nil {
				http.Error(w, err.Error(), http.StatusUnauthorized)
				return
			}
			if _, err := io.WriteString(w, id.String()); err != nil {
				t.Error(err)
			}
		}),
	}
	served := make(chan error, 1)
	go func() { served <- srv.Serve(tls.NewListener(lis, cfg)) }()
	t.Cleanup(func() {
		require.NoError(t, srv.Close())
		require.ErrorIs(t, <-served, http.ErrServerClosed)
	})
	return "https://" + lis.Addr().String()
}

// satelliteGet calls url with svid as the client certificate, trusting ca.
func satelliteGet(t *testing.T, ca *spiffetest.CA, svid *x509svid.SVID, url string) (string, error) {
	t.Helper()
	client := &http.Client{Timeout: 5 * time.Second, Transport: &http.Transport{
		TLSClientConfig: tlsconfig.MTLSClientConfig(svid, ca.Bundle(), tlsconfig.AuthorizeID(spiffeid.RequireFromPath(ca.TrustDomain, gcPath))),
	}}
	resp, err := client.Get(url) //nolint:noctx // test client
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	return string(body), err
}

func TestStaticProviderServesMutualTLS(t *testing.T) {
	ca := spiffetest.NewCA(t, ourTD.String())
	cert, key, bundle := ca.WriteFiles(t, t.TempDir(), ca.SVID(t, gcPath))

	p, err := NewStaticProvider(cert, key, bundle, ourTD.String())
	require.NoError(t, err)
	require.Equal(t, ourTD, p.GetTrustDomain())
	_, err = p.GetX509Source(context.Background())
	require.ErrorContains(t, err, "not available for static provider")
	require.NoError(t, p.Close())

	cfg, err := p.GetTLSConfig(context.Background(), NewSatelliteAuthorizer(ourTD).AuthorizeID())
	require.NoError(t, err)
	url := serveTLS(t, cfg)

	body, err := satelliteGet(t, ca, ca.SVID(t, "/satellite/region/eu/edge-01"), url)
	require.NoError(t, err)
	require.Equal(t, "spiffe://harbor-satellite.test/satellite/region/eu/edge-01", body)

	_, err = satelliteGet(t, ca, ca.SVID(t, "/agent/edge-01"), url)
	require.Error(t, err, "a non-satellite SVID must not get through the satellite authorizer")

	stranger := spiffetest.NewCA(t, ourTD.String())
	_, err = satelliteGet(t, ca, stranger.SVID(t, "/satellite/region/eu/edge-01"), url)
	require.Error(t, err, "an SVID from another CA of the same trust domain name must be rejected")
}

func TestStaticProviderErrors(t *testing.T) {
	ca := spiffetest.NewCA(t, ourTD.String())
	cert, key, bundle := ca.WriteFiles(t, t.TempDir(), ca.SVID(t, gcPath))
	missing := filepath.Join(t.TempDir(), "missing.pem")

	_, err := NewStaticProvider(cert, key, bundle, "Bad Domain")
	require.ErrorContains(t, err, "invalid trust domain")
	_, err = NewStaticProvider(missing, key, bundle, ourTD.String())
	require.ErrorContains(t, err, "load SVID")
	_, err = NewStaticProvider(cert, key, missing, ourTD.String())
	require.ErrorContains(t, err, "load bundle")
}

func TestNewProviderFromEnvironment(t *testing.T) {
	prev := env.GC.SPIFFE
	t.Cleanup(func() { env.GC.SPIFFE = prev })
	ca := spiffetest.NewCA(t, ourTD.String())
	cert, key, bundle := ca.WriteFiles(t, t.TempDir(), ca.SVID(t, gcPath))

	env.GC.SPIFFE = env.SPIFFE{Enabled: true, TrustDomain: ourTD.String(), Provider: "static", CertFile: cert, KeyFile: key, BundleFile: bundle}
	cfg := LoadConfig()
	require.Equal(t, &Config{Enabled: true, TrustDomain: ourTD.String(), ProviderType: "static"}, cfg)
	p, err := NewProvider(cfg)
	require.NoError(t, err)
	require.IsType(t, &StaticProvider{}, p)

	env.GC.SPIFFE.KeyFile = ""
	_, err = NewProvider(LoadConfig())
	require.ErrorContains(t, err, "SPIFFE_KEY_FILE")

	p, err = NewProvider(&Config{ProviderType: "sidecar", TrustDomain: ourTD.String()})
	require.NoError(t, err)
	require.IsType(t, &SidecarProvider{}, p)
	require.NoError(t, p.Close(), "closing a sidecar provider that never connected")

	_, err = NewProvider(&Config{ProviderType: "sidecar", TrustDomain: "Bad Domain"})
	require.ErrorContains(t, err, "invalid trust domain")
	_, err = NewProvider(&Config{ProviderType: "vault"})
	require.ErrorContains(t, err, "unknown provider type: vault")
}

func sidecar(t *testing.T, ca *spiffetest.CA) (*SidecarProvider, *spiffetest.WorkloadAPI) {
	t.Helper()
	agent := spiffetest.StartWorkloadAPI(t, ca, ca.SVID(t, gcPath))
	p, err := NewSidecarProvider(&Config{TrustDomain: ourTD.String(), EndpointSocket: agent.Addr})
	require.NoError(t, err)
	return p, agent
}

func TestSidecarProviderAndSource(t *testing.T) {
	ca := spiffetest.NewCA(t, ourTD.String())
	p, agent := sidecar(t, ca)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	src, err := NewSource(ctx, p)
	require.NoError(t, err)
	require.Equal(t, ourTD, src.GetTrustDomain())
	id, err := src.GetSPIFFEID()
	require.NoError(t, err)
	require.Equal(t, "spiffe://harbor-satellite.test/gc/main", id.String())
	svid, err := src.GetSVID()
	require.NoError(t, err)
	require.Equal(t, id, svid.ID)

	server, err := src.ServerTLSConfig(NewSatelliteAuthorizer(ourTD).AuthorizeID())
	require.NoError(t, err)
	url := serveTLS(t, server)
	body, err := satelliteGet(t, ca, ca.SVID(t, "/satellite/region/eu/edge-01"), url)
	require.NoError(t, err)
	require.Equal(t, "spiffe://harbor-satellite.test/satellite/region/eu/edge-01", body)

	providerCfg, err := p.GetTLSConfig(ctx, nil)
	require.NoError(t, err)
	body, err = satelliteGet(t, ca, ca.SVID(t, "/agent/x"), serveTLS(t, providerCfg))
	require.NoError(t, err, "a nil authorizer accepts any SVID of the bundle")
	require.Equal(t, "spiffe://harbor-satellite.test/agent/x", body)

	// Ground Control as a client of another SPIFFE server.
	client, err := src.ClientTLSConfig(nil)
	require.NoError(t, err)
	require.NotNil(t, client.GetClientCertificate)

	agent.Rotate(ca.SVID(t, "/gc/rotated"))
	require.Eventually(t, func() bool {
		id, err := src.GetSPIFFEID()
		return err == nil && id.Path() == "/gc/rotated"
	}, 5*time.Second, 20*time.Millisecond, "the source follows SVID rotation")

	require.NoError(t, src.Close())
	require.NoError(t, src.Close())
	_, err = src.GetSVID()
	require.ErrorContains(t, err, "source is closed")
	_, err = src.GetSPIFFEID()
	require.ErrorContains(t, err, "source is closed")
	_, err = src.ServerTLSConfig(nil)
	require.ErrorContains(t, err, "source is closed")
	_, err = src.ClientTLSConfig(nil)
	require.ErrorContains(t, err, "source is closed")
}

func TestNewSourceFailsWithoutAnAgent(t *testing.T) {
	p, err := NewSidecarProvider(&Config{TrustDomain: ourTD.String(), EndpointSocket: "unix:///nonexistent/agent.sock"})
	require.NoError(t, err)
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	_, err = NewSource(ctx, p)
	require.ErrorContains(t, err, "create X509Source")

	_, err = NewSource(context.Background(), &StaticProvider{})
	require.ErrorContains(t, err, "get X509Source from provider")
}
