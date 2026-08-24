package events

import (
	"crypto/tls"
	"net/http"
	"testing"
	"time"

	"github.com/container-registry/harbor-satellite/pkg/config"
)

func TestCreateHTTPClient_Unsecure(t *testing.T) {
	client, err := createHTTPClient(config.TLSConfig{}, true)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if client == nil {
		t.Fatal("expected non-nil client")
	}
	if client.Timeout != 30*time.Second {
		t.Errorf("expected timeout 30s, got %v", client.Timeout)
	}

	transport, ok := client.Transport.(*http.Transport)
	if !ok {
		t.Fatal("expected *http.Transport")
	}
	if transport.TLSClientConfig == nil {
		t.Fatal("expected TLSClientConfig to be set when useUnsecure=true")
	}
	if !transport.TLSClientConfig.InsecureSkipVerify {
		t.Error("expected InsecureSkipVerify to be true when useUnsecure=true")
	}
	if transport.TLSClientConfig.MinVersion != tls.VersionTLS12 {
		t.Errorf("expected MinVersion TLS1.2, got %v", transport.TLSClientConfig.MinVersion)
	}
}

func TestCreateHTTPClient_NoTLSConfig(t *testing.T) {
	client, err := createHTTPClient(config.TLSConfig{}, false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if client == nil {
		t.Fatal("expected non-nil client")
	}

	transport, ok := client.Transport.(*http.Transport)
	if !ok {
		t.Fatal("expected *http.Transport")
	}
	if transport.TLSClientConfig != nil {
		t.Errorf("expected nil TLSClientConfig when no cert/CA and not unsecure, got %+v", transport.TLSClientConfig)
	}
	if transport.MaxIdleConns != 10 {
		t.Errorf("expected MaxIdleConns 10, got %d", transport.MaxIdleConns)
	}
	if transport.IdleConnTimeout != 30*time.Second {
		t.Errorf("expected IdleConnTimeout 30s, got %v", transport.IdleConnTimeout)
	}
	if !transport.DisableCompression {
		t.Error("expected DisableCompression to be true")
	}
}

func TestCreateHTTPClient_InvalidCertPaths(t *testing.T) {
	tlsCfg := config.TLSConfig{
		CertFile: "/nonexistent/cert.pem",
		KeyFile:  "/nonexistent/key.pem",
		CAFile:   "/nonexistent/ca.pem",
	}

	client, err := createHTTPClient(tlsCfg, false)
	if err == nil {
		t.Fatal("expected error for nonexistent cert/key/CA files, got nil")
	}
	if client != nil {
		t.Errorf("expected nil client on error, got %+v", client)
	}
}

func TestCreateHTTPClient_CAFileOnlyTriggersLoad(t *testing.T) {
	// CAFile alone (no CertFile) should still enter the TLS-loading branch
	// and fail since the file doesn't exist.
	tlsCfg := config.TLSConfig{
		CAFile: "/nonexistent/ca.pem",
	}

	_, err := createHTTPClient(tlsCfg, false)
	if err == nil {
		t.Fatal("expected error for nonexistent CA file, got nil")
	}
}
