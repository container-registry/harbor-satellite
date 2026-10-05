package root

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/container-registry/harbor-satellite/internal/groundcontrol/server"
	"github.com/container-registry/harbor-satellite/internal/groundcontrol/spiffe"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/require"
)

func TestServeInitialization(t *testing.T) {
	t.Parallel()
	for _, failAt := range []string{"environment", "health", "migrations", "server"} {
		t.Run(failAt, func(t *testing.T) {
			t.Parallel()
			failure := errors.New("startup failed")
			var calls []string
			step := func(name string) error {
				calls = append(calls, name)
				if name == failAt {
					return failure
				}
				return nil
			}
			ctx := t.Context()
			deps := serveDependencies{
				loadEnvironment: func() error { return step("environment") },
				checkHealth: func(got context.Context) error {
					require.Equal(t, ctx, got)
					return step("health")
				},
				migrate: func(got context.Context) error {
					require.Equal(t, ctx, got)
					return step("migrations")
				},
				newServer: func(got context.Context) (*server.ServerResult, error) {
					require.Equal(t, ctx, got)
					return nil, step("server")
				},
			}
			rootCmd := &cobra.Command{Use: "groundcontrol", PersistentPreRunE: func(_ *cobra.Command, _ []string) error {
				t.Fatal("administration client initialized for serve")
				return nil
			}}
			rootCmd.AddCommand(newServeCommand(deps))
			rootCmd.SetArgs([]string{"serve"})
			rootCmd.SetOut(io.Discard)
			rootCmd.SetErr(io.Discard)
			require.ErrorIs(t, rootCmd.ExecuteContext(ctx), failure)
			order := []string{"environment", "health", "migrations", "server"}
			for i, name := range order {
				if name == failAt {
					require.Equal(t, order[:i+1], calls)
				}
			}
		})
	}
}

func TestServeAlreadyCancelled(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	cmd := newServeCommand(serveDependencies{})
	require.ErrorIs(t, cmd.ExecuteContext(ctx), context.Canceled)
}

func TestServeListenerFailure(t *testing.T) {
	t.Parallel()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	defer listener.Close()
	result := &server.ServerResult{Server: &http.Server{Addr: listener.Addr().String()}, AppServer: &server.Server{}}
	err = serveHTTP(t.Context(), io.Discard, result, time.Second)
	require.ErrorContains(t, err, "cannot start server")
}

func TestServeTLSListenerFailure(t *testing.T) {
	t.Parallel()
	result := &server.ServerResult{
		Server: &http.Server{Addr: "127.0.0.1:0"}, AppServer: &server.Server{},
		TLSConfig: &server.ServerTLSConfig{Enabled: true, CertFile: "missing.crt", KeyFile: "missing.key"},
	}
	require.ErrorContains(t, serveHTTP(t.Context(), io.Discard, result, time.Second), "missing.crt")
}

func TestServeGracefulShutdown(t *testing.T) {
	t.Parallel()
	for _, mode := range []string{"http", "tls", "spiffe"} {
		t.Run(mode, func(t *testing.T) {
			t.Parallel()
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			started := make(chan string, 1)
			entered := make(chan struct{})
			release := make(chan struct{})
			defer close(release)
			result := &server.ServerResult{AppServer: &server.Server{}, Server: &http.Server{
				Addr: "127.0.0.1:0", ReadHeaderTimeout: time.Second,
				BaseContext: func(listener net.Listener) context.Context {
					started <- listener.Addr().String()
					return context.Background()
				},
				Handler: http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
					close(entered)
					<-release
					_, err := w.Write([]byte("finished"))
					if err != nil {
						t.Errorf("write response: %v", err)
					}
				}),
			}}
			scheme := "http"
			client := &http.Client{Timeout: 3 * time.Second}
			if mode != "http" {
				fixture := httptest.NewTLSServer(http.NotFoundHandler())
				defer fixture.Close()
				certificate := fixture.TLS.Certificates[0]
				result.Server.TLSConfig = &tls.Config{
					MinVersion:     tls.VersionTLS12,
					GetCertificate: func(_ *tls.ClientHelloInfo) (*tls.Certificate, error) { return &certificate, nil },
				}
				if mode == "spiffe" {
					result.SPIFFEConfig = &spiffe.Config{Enabled: true}
				} else {
					certFile := filepath.Join(t.TempDir(), "server.crt")
					keyFile := filepath.Join(t.TempDir(), "server.key")
					key, err := x509.MarshalPKCS8PrivateKey(certificate.PrivateKey)
					require.NoError(t, err)
					require.NoError(t, os.WriteFile(certFile, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: certificate.Certificate[0]}), 0o600))
					require.NoError(t, os.WriteFile(keyFile, pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: key}), 0o600))
					result.Server.TLSConfig = nil
					result.TLSConfig = &server.ServerTLSConfig{Enabled: true, CertFile: certFile, KeyFile: keyFile}
				}
				client = fixture.Client()
				client.Timeout = 3 * time.Second
				scheme = "https"
			}
			defer client.CloseIdleConnections()
			done := make(chan error, 1)
			go func() { done <- serveHTTP(ctx, io.Discard, result, time.Second) }()
			address := receive(t, started)
			response := make(chan string, 1)
			go func() {
				res, err := client.Get(scheme + "://" + address)
				if err != nil {
					response <- err.Error()
					return
				}
				defer res.Body.Close()
				body, err := io.ReadAll(res.Body)
				if err != nil {
					response <- err.Error()
					return
				}
				response <- string(body)
			}()
			receive(t, entered)
			cancel()
			select {
			case err := <-done:
				t.Fatalf("shutdown did not wait for active request: %v", err)
			case <-time.After(20 * time.Millisecond):
			}
			release <- struct{}{}
			require.Equal(t, "finished", receive(t, response))
			require.NoError(t, receive(t, done))
		})
	}
}

func TestServeShutdownTimeoutClosesActiveRequests(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	started := make(chan string, 1)
	entered := make(chan struct{})
	requestClosed := make(chan struct{})
	result := &server.ServerResult{AppServer: &server.Server{}, Server: &http.Server{
		Addr: "127.0.0.1:0", ReadHeaderTimeout: time.Second,
		BaseContext: func(listener net.Listener) context.Context {
			started <- listener.Addr().String()
			return context.Background()
		},
		Handler: http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
			close(entered)
			<-r.Context().Done()
			close(requestClosed)
		}),
	}}
	done := make(chan error, 1)
	go func() { done <- serveHTTP(ctx, io.Discard, result, 20*time.Millisecond) }()
	address := receive(t, started)
	clientDone := make(chan struct{})
	go func() {
		defer close(clientDone)
		client := &http.Client{Timeout: 3 * time.Second}
		defer client.CloseIdleConnections()
		res, err := client.Get("http://" + address)
		if err == nil {
			_ = res.Body.Close()
		}
	}()
	receive(t, entered)
	cancel()
	require.ErrorIs(t, receive(t, done), context.DeadlineExceeded)
	receive(t, requestClosed)
	receive(t, clientDone)
}

func TestServeCancellationDuringInitialization(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	cmd := newServeCommand(serveDependencies{
		loadEnvironment: func() error { return nil },
		checkHealth: func(ctx context.Context) error {
			cancel()
			return ctx.Err()
		},
	})
	require.ErrorIs(t, cmd.ExecuteContext(ctx), context.Canceled)
}

func receive[T any](t *testing.T, ch <-chan T) T {
	t.Helper()
	select {
	case result := <-ch:
		return result
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for server")
	}
	var zero T
	return zero
}
