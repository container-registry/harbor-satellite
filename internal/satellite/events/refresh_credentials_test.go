package events

import (
	"context"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"sync"
	"testing"

	"github.com/container-registry/harbor-satellite/pkg/config"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/require"
)

func newTestRefreshProcess() *RefreshCredentialProcess {
	return &RefreshCredentialProcess{
		name:       "refresh_credentials",
		isRunning:  false,
		isComplete: false,
	}
}

func TestRefreshCredentialProcess_Name(t *testing.T) {
	p := newTestRefreshProcess()

	if got := p.Name(); got != "refresh_credentials" {
		t.Errorf("expected name %q, got %q", "refresh_credentials", got)
	}
}

func TestRefreshCredentialProcess_IsComplete_Default(t *testing.T) {
	p := newTestRefreshProcess()

	if p.IsComplete() {
		t.Error("expected IsComplete to be false by default")
	}

	p.isComplete = true
	if !p.IsComplete() {
		t.Error("expected IsComplete to reflect true after being set")
	}
}

func TestRefreshCredentialProcess_IsRunning_Default(t *testing.T) {
	p := newTestRefreshProcess()

	if p.IsRunning() {
		t.Error("expected IsRunning to be false by default")
	}
}

func TestRefreshCredentialProcess_StartStop(t *testing.T) {
	p := newTestRefreshProcess()

	p.start()
	if !p.IsRunning() {
		t.Error("expected IsRunning to be true after start()")
	}

	p.stop()
	if p.IsRunning() {
		t.Error("expected IsRunning to be false after stop()")
	}
}

func TestRefreshCredentialProcess_StartStop_Concurrent(t *testing.T) {
	p := newTestRefreshProcess()

	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(2)
		go func() {
			defer wg.Done()
			p.start()
		}()
		go func() {
			defer wg.Done()
			_ = p.IsRunning()
		}()
	}
	wg.Wait()
	p.stop()

	if p.IsRunning() {
		t.Error("expected IsRunning to be false after final stop()")
	}
}

func TestExecute_PostsRefreshToSatellitesRefresh(t *testing.T) {
	tests := []struct {
		name    string
		baseURL func(serverURL string) string
	}{
		{
			name:    "url without trailing slash",
			baseURL: func(serverURL string) string { return serverURL },
		},
		{
			name:    "url with trailing slash",
			baseURL: func(serverURL string) string { return serverURL + "/" },
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var gotMethod, gotPath, gotUser, gotPass string
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				gotMethod = r.Method
				gotPath = r.URL.Path
				gotUser, gotPass, _ = r.BasicAuth()
				w.Header().Set("Content-Type", "application/json")
				if _, err := w.Write([]byte(`{"secret":"new-secret"}`)); err != nil {
					t.Errorf("write refresh response: %v", err)
				}
			}))
			t.Cleanup(srv.Close)

			cfgPath := filepath.Join(t.TempDir(), "config.json")
			cm, err := config.NewConfigManager(cfgPath, "", "", tt.baseURL(srv.URL), false, &config.Config{})
			require.NoError(t, err)
			cm.With(
				config.SetUseUnsecure(true),
				config.SetStateAuth("robot$sat", "old-secret", config.URL("http://registry.example")),
			)

			log := zerolog.Nop()
			proc := &RefreshCredentialProcess{
				name: "refresh_credentials",
				cm:   cm,
				log:  &log,
			}

			require.NoError(t, proc.Execute(context.Background()))
			require.Equal(t, http.MethodPost, gotMethod)
			require.Equal(t, "/satellites/refresh", gotPath)
			require.Equal(t, "robot$sat", gotUser)
			require.Equal(t, "old-secret", gotPass)
			require.Equal(t, "new-secret", cm.GetSourceRegistryPassword())
		})
	}
}

func TestCreateRequest_RejectsPlainHTTPWhenSecure(t *testing.T) {
	cm, err := config.NewConfigManager(filepath.Join(t.TempDir(), "config.json"), "", "", "http://ground-control", false, &config.Config{})
	require.NoError(t, err)
	cm.With(config.SetStateAuth("robot$sat", "secret", config.URL("http://registry.example")))

	log := zerolog.Nop()
	proc := &RefreshCredentialProcess{cm: cm, log: &log}
	_, err = proc.createRequest(context.Background(), "http://ground-control/satellites/refresh")
	require.Error(t, err)
	require.Contains(t, err.Error(), "must use HTTPS")
}
