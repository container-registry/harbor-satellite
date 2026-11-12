// Package spiffetest issues X.509 SVIDs from a throwaway CA and serves them
// over a fake SPIFFE Workload API, for tests of SPIFFE clients and servers.
package spiffetest

import (
	"context"
	"crypto"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/spiffe/go-spiffe/v2/bundle/x509bundle"
	"github.com/spiffe/go-spiffe/v2/proto/spiffe/workload"
	"github.com/spiffe/go-spiffe/v2/spiffeid"
	"github.com/spiffe/go-spiffe/v2/svid/x509svid"
	"google.golang.org/grpc"
)

// CA is a self-signed root for one trust domain.
type CA struct {
	TrustDomain spiffeid.TrustDomain
	cert        *x509.Certificate
	key         crypto.Signer
}

// NewCA creates a CA for trustDomain.
func NewCA(t testing.TB, trustDomain string) *CA {
	t.Helper()
	td, err := spiffeid.TrustDomainFromString(trustDomain)
	if err != nil {
		t.Fatal(err)
	}
	key := newKey(t)
	tmpl := &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: "spiffetest CA"},
		NotBefore:             time.Now().Add(-time.Minute),
		NotAfter:              time.Now().Add(time.Hour),
		IsCA:                  true,
		BasicConstraintsValid: true,
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageCRLSign,
		URIs:                  []*url.URL{td.ID().URL()},
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, key.Public(), key)
	if err != nil {
		t.Fatal(err)
	}
	cert, err := x509.ParseCertificate(der)
	if err != nil {
		t.Fatal(err)
	}
	return &CA{TrustDomain: td, cert: cert, key: key}
}

func newKey(t testing.TB) *ecdsa.PrivateKey {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	return key
}

// Bundle is the CA as a trust bundle of its trust domain.
func (ca *CA) Bundle() *x509bundle.Bundle {
	return x509bundle.FromX509Authorities(ca.TrustDomain, []*x509.Certificate{ca.cert})
}

// SVID issues an X.509 SVID for path in the CA's trust domain.
func (ca *CA) SVID(t testing.TB, path string) *x509svid.SVID {
	t.Helper()
	id, err := spiffeid.FromPath(ca.TrustDomain, path)
	if err != nil {
		t.Fatal(err)
	}
	key := newKey(t)
	serial, err := rand.Int(rand.Reader, big.NewInt(1<<62))
	if err != nil {
		t.Fatal(err)
	}
	tmpl := &x509.Certificate{
		SerialNumber: serial,
		NotBefore:    time.Now().Add(-time.Minute),
		NotAfter:     time.Now().Add(time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment | x509.KeyUsageKeyAgreement,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth, x509.ExtKeyUsageClientAuth},
		URIs:         []*url.URL{id.URL()},
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, ca.cert, key.Public(), ca.key)
	if err != nil {
		t.Fatal(err)
	}
	cert, err := x509.ParseCertificate(der)
	if err != nil {
		t.Fatal(err)
	}
	return &x509svid.SVID{ID: id, Certificates: []*x509.Certificate{cert}, PrivateKey: key}
}

// WriteFiles writes svid, its key and the CA bundle as PEM files into dir
// and returns their paths.
func (ca *CA) WriteFiles(t testing.TB, dir string, svid *x509svid.SVID) (certFile, keyFile, bundleFile string) {
	t.Helper()
	certPEM, keyPEM, err := svid.Marshal()
	if err != nil {
		t.Fatal(err)
	}
	certFile = filepath.Join(dir, "svid.pem")
	keyFile = filepath.Join(dir, "svid.key")
	bundleFile = filepath.Join(dir, "bundle.pem")
	bundlePEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: ca.cert.Raw})
	for path, data := range map[string][]byte{certFile: certPEM, keyFile: keyPEM, bundleFile: bundlePEM} {
		if err := os.WriteFile(path, data, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return certFile, keyFile, bundleFile
}

// WorkloadAPI serves X.509 SVIDs the way a SPIRE agent does.
type WorkloadAPI struct {
	workload.UnimplementedSpiffeWorkloadAPIServer

	// Addr is the socket address for workloadapi.WithAddr.
	Addr string

	ca      *CA
	mu      sync.Mutex
	svid    *x509svid.SVID
	updates chan struct{}
}

// StartWorkloadAPI serves svid on a fresh unix socket until the test ends.
func StartWorkloadAPI(t testing.TB, ca *CA, svid *x509svid.SVID) *WorkloadAPI {
	t.Helper()
	// t.TempDir paths on macOS are longer than the 104 bytes a unix socket
	// path may have.
	dir, err := os.MkdirTemp("", "wl")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	socket := filepath.Join(dir, "agent.sock")
	lis, err := (&net.ListenConfig{}).Listen(context.Background(), "unix", socket)
	if err != nil {
		t.Fatal(err)
	}
	w := &WorkloadAPI{Addr: "unix://" + socket, ca: ca, svid: svid, updates: make(chan struct{})}
	g := grpc.NewServer()
	workload.RegisterSpiffeWorkloadAPIServer(g, w)
	served := make(chan error, 1)
	go func() { served <- g.Serve(lis) }()
	t.Cleanup(func() {
		g.Stop()
		if err := <-served; err != nil {
			t.Errorf("workload API fake: %v", err)
		}
	})
	return w
}

// Rotate replaces the served SVID and pushes it to connected workloads.
func (w *WorkloadAPI) Rotate(svid *x509svid.SVID) {
	w.mu.Lock()
	w.svid = svid
	old := w.updates
	w.updates = make(chan struct{})
	w.mu.Unlock()
	close(old)
}

func (w *WorkloadAPI) current() (*workload.X509SVIDResponse, <-chan struct{}, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	key, err := x509.MarshalPKCS8PrivateKey(w.svid.PrivateKey)
	if err != nil {
		return nil, nil, err
	}
	var chain []byte
	for _, c := range w.svid.Certificates {
		chain = append(chain, c.Raw...)
	}
	return &workload.X509SVIDResponse{Svids: []*workload.X509SVID{{
		SpiffeId:    w.svid.ID.String(),
		X509Svid:    chain,
		X509SvidKey: key,
		Bundle:      w.ca.cert.Raw,
	}}}, w.updates, nil
}

func (w *WorkloadAPI) FetchX509SVID(_ *workload.X509SVIDRequest, stream grpc.ServerStreamingServer[workload.X509SVIDResponse]) error {
	for {
		resp, updated, err := w.current()
		if err != nil {
			return err
		}
		if err := stream.Send(resp); err != nil {
			return err
		}
		select {
		case <-updated:
		case <-stream.Context().Done():
			return nil
		}
	}
}
