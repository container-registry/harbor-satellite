package main

import (
	"context"
	"errors"
	"net"
	"net/http"
	"path/filepath"
	"testing"
	"time"

	"github.com/container-registry/harbor-satellite/internal/satellite"
	runtime "github.com/container-registry/harbor-satellite/internal/satellite/container_runtime"
	"github.com/container-registry/harbor-satellite/internal/satellite/proxy"
	"github.com/container-registry/harbor-satellite/internal/satellite/store"
	"github.com/container-registry/harbor-satellite/pkg/config"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/require"
	"golang.org/x/sync/errgroup"
)

func newTestConfigManager(t *testing.T, cfg *config.Config) *config.ConfigManager {
	t.Helper()
	dir := t.TempDir()
	configPath := filepath.Join(dir, "config.json")
	prevPath := filepath.Join(dir, "prev_config.json")
	cm, err := config.NewConfigManager(configPath, prevPath, "token", "http://gc:8080", false, cfg)
	require.NoError(t, err)

	return cm
}

func TestResolveLocalRegistryEndpointInvalidMode(t *testing.T) {
	endpoint := resolveLocalRegistryEndpoint("", config.DefaultProxyPort)
	require.Empty(t, endpoint)
}

func TestResolveLocalRegistryEndpoint(t *testing.T) {
	for _, mode := range []proxy.Mode{proxy.ModeProxy, proxy.ModeReplica} {
		require.Equal(t, "127.0.0.1:9090", resolveLocalRegistryEndpoint(mode, 9090))
	}
}

func TestValidateSatelliteOptions(t *testing.T) {
	tests := []struct {
		name    string
		opts    SatelliteOptions
		wantErr string
	}{
		{name: "proxy mode", opts: SatelliteOptions{ProxyMode: proxy.ModeProxy, ProxyPort: 8585}},
		{name: "replica mode", opts: SatelliteOptions{ProxyMode: proxy.ModeReplica, ProxyPort: 8585}},
		{name: "invalid mode", opts: SatelliteOptions{ProxyMode: "cache", ProxyPort: 8585}, wantErr: "must be"},
		{name: "zero port", opts: SatelliteOptions{ProxyMode: proxy.ModeProxy}, wantErr: "between 1 and 65535"},
		{name: "port too large", opts: SatelliteOptions{ProxyMode: proxy.ModeProxy, ProxyPort: 65536}, wantErr: "between 1 and 65535"},
		{name: "fallback only", opts: SatelliteOptions{ProxyMode: proxy.ModeProxy, ProxyPort: 8585, FallbackOnly: true}, wantErr: "cannot be combined"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateSatelliteOptions(tt.opts)
			if tt.wantErr == "" {
				require.NoError(t, err)
				return
			}
			require.ErrorContains(t, err, tt.wantErr)
		})
	}
}

func TestSourceRegistryOptionsUsesHarborOverride(t *testing.T) {
	cm := newTestConfigManager(t, &config.Config{
		StateConfig: config.StateConfig{
			RegistryCredentials: config.RegistryCredentials{
				URL:      "http://registry.internal:5000",
				Username: "old-user",
				Password: "old-password",
			},
		},
		AppConfig: config.AppConfig{
			UseUnsecure:       true,
			HarborRegistryURL: "https://harbor.example:8443",
		},
	})
	options, err := sourceRegistryOptions(cm)
	require.NoError(t, err)
	require.Equal(t, "harbor.example:8443", options.Endpoint)
	require.Equal(t, "old-user", options.Username)
	require.Equal(t, "old-password", options.Password)
	require.True(t, options.PlainHTTP)
}

func TestProxyStoresPrioritizesBYORegistry(t *testing.T) {
	cm := newTestConfigManager(t, &config.Config{
		StateConfig: config.StateConfig{RegistryCredentials: config.RegistryCredentials{URL: "http://harbor.example.com"}},
		AppConfig: config.AppConfig{
			BringOwnRegistry:         true,
			UseUnsecure:              true,
			LocalRegistryCredentials: config.RegistryCredentials{URL: "http://byor.example.com"},
		},
	})
	local, remote, err := proxyStores(cm, t.TempDir())
	require.NoError(t, err)
	require.IsType(t, &store.RegistryStore{}, local)
	require.IsType(t, &store.RegistryStore{}, remote)
}

func TestGracefulShutdownDrainsActiveProxyResponse(t *testing.T) {
	requestStarted := make(chan struct{})
	releaseRequest := make(chan struct{})
	server, listener, group := startTestProxyServer(t, http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
		close(requestStarted)
		<-releaseRequest
		response.WriteHeader(http.StatusNoContent)
	}))

	clientDone := make(chan error, 1)
	go func() {
		response, err := http.Get("http://" + listener.Addr().String() + "/v2/") //nolint:noctx // test request lifetime is controlled by the server gate.
		if err == nil {
			err = response.Body.Close()
		}
		clientDone <- err
	}()
	<-requestStarted

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	log := zerolog.Nop()
	shutdownDone := make(chan error, 1)
	go func() {
		shutdownDone <- gracefulShutdown(ctx, &log, &satellite.Satellite{}, server, group, "1s")
	}()

	select {
	case err := <-shutdownDone:
		t.Fatalf("shutdown returned before the active response drained: %v", err)
	case <-time.After(30 * time.Millisecond):
	}
	close(releaseRequest)
	require.NoError(t, <-clientDone)
	require.NoError(t, <-shutdownDone)
}

func TestGracefulShutdownTimesOutStuckProxyResponse(t *testing.T) {
	requestStarted := make(chan struct{})
	releaseRequest := make(chan struct{})
	server, listener, group := startTestProxyServer(t, http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
		close(requestStarted)
		<-releaseRequest
		response.WriteHeader(http.StatusNoContent)
	}))

	clientDone := make(chan struct{})
	go func() {
		response, err := http.Get("http://" + listener.Addr().String() + "/v2/") //nolint:noctx // test request lifetime is controlled by the server gate.
		if err == nil {
			_ = response.Body.Close()
		}
		close(clientDone)
	}()
	<-requestStarted

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	log := zerolog.Nop()
	err := gracefulShutdown(ctx, &log, &satellite.Satellite{}, server, group, "20ms")
	require.ErrorContains(t, err, "graceful shutdown timeout exceeded")

	close(releaseRequest)
	select {
	case <-clientDone:
	case <-time.After(time.Second):
		t.Fatal("stuck proxy request did not exit after it was released")
	}
}

func TestGracefulShutdownReturnsRuntimeError(t *testing.T) {
	want := errors.New("watcher failed")
	group := &errgroup.Group{}
	group.Go(func() error { return want })
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	log := zerolog.Nop()

	err := gracefulShutdown(ctx, &log, &satellite.Satellite{}, nil, group, "1s")
	require.ErrorIs(t, err, want)
}

func startTestProxyServer(
	t *testing.T,
	handler http.Handler,
) (*http.Server, net.Listener, *errgroup.Group) {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	server := &http.Server{Handler: handler}
	group := &errgroup.Group{}
	group.Go(func() error {
		err := server.Serve(listener)
		if err == nil || errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	})
	t.Cleanup(func() {
		_ = server.Close()
	})
	return server, listener, group
}

func TestResolveCRIAndApply(t *testing.T) {
	t.Run("local OCI store does not configure registry fallback", func(t *testing.T) {
		cfg := &config.Config{
			AppConfig: config.AppConfig{
				RegistryFallback: config.RegistryFallbackConfig{Enabled: true},
			},
		}
		cm := newTestConfigManager(t, cfg)

		results := resolveCRIAndApply(cm, nil, false, "")
		require.Nil(t, results)
	})

	t.Run("noFallback returns nil", func(t *testing.T) {
		cfg := &config.Config{}
		cm := newTestConfigManager(t, cfg)

		results := resolveCRIAndApply(cm, nil, true, "localhost:8585")
		require.Nil(t, results)
	})

	t.Run("no config no mirrors returns nil", func(t *testing.T) {
		cfg := &config.Config{}
		cm := newTestConfigManager(t, cfg)

		results := resolveCRIAndApply(cm, nil, false, "localhost:8585")
		require.Nil(t, results)
	})

	t.Run("config file wins over mirrors", func(t *testing.T) {
		cfg := &config.Config{
			AppConfig: config.AppConfig{
				RegistryFallback: config.RegistryFallbackConfig{
					Enabled:    true,
					Registries: []string{"docker.io"},
					Runtimes:   []string{"unsupported_cri"},
				},
			},
		}
		cm := newTestConfigManager(t, cfg)

		mirrors := mirrorFlags{"containerd:quay.io"}
		results := resolveCRIAndApply(cm, mirrors, false, "localhost:8585")
		require.Len(t, results, 1)
		require.Equal(t, runtime.CRIType("unsupported_cri"), results[0].CRI)
		require.False(t, results[0].Success)
	})

	t.Run("mirrors used when config disabled", func(t *testing.T) {
		cfg := &config.Config{}
		cm := newTestConfigManager(t, cfg)

		mirrors := mirrorFlags{"badformat"}
		results := resolveCRIAndApply(cm, mirrors, false, "localhost:8585")
		require.Nil(t, results)
	})
}

func TestMirrorFlags(t *testing.T) {
	t.Run("Set accumulates values", func(t *testing.T) {
		var m mirrorFlags
		require.NoError(t, m.Set("containerd:docker.io"))
		require.NoError(t, m.Set("docker:true"))
		require.Len(t, m, 2)
		require.Equal(t, "containerd:docker.io", m[0])
		require.Equal(t, "docker:true", m[1])
	})

	t.Run("String returns formatted output", func(t *testing.T) {
		m := mirrorFlags{"containerd:docker.io", "docker:true"}
		require.Equal(t, "[containerd:docker.io docker:true]", m.String())
	})

	t.Run("empty String", func(t *testing.T) {
		var m mirrorFlags
		require.Equal(t, "[]", m.String())
	})
}
