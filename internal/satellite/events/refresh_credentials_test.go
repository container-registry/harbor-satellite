package events

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"sync"
	"testing"

	"github.com/container-registry/harbor-satellite/internal/shared/logger/logtest"
	"github.com/container-registry/harbor-satellite/internal/shared/testutil/pending"
	"github.com/container-registry/harbor-satellite/pkg/config"
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

func TestRefreshCredentialProcess_ExecuteDoesNotLogSecrets(t *testing.T) {
	pending.Plan(t, "D4", "credential refresh logs the response body with the new secret")

	const (
		currentSecret   = "d4-refresh-current-robot-secret"
		refreshedSecret = "d4-refresh-new-robot-secret"
	)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, err := w.Write([]byte(`{"secret":"` + refreshedSecret + `"}`))
		require.NoError(t, err)
	}))
	defer srv.Close()

	dir := t.TempDir()
	cm, err := config.NewConfigManager(
		filepath.Join(dir, "config.json"),
		filepath.Join(dir, "prev.json"),
		"", srv.URL, false,
		&config.Config{
			StateConfig: config.StateConfig{
				RegistryCredentials: config.RegistryCredentials{
					URL:      "http://registry",
					Username: "robot$test-sat",
					Password: currentSecret,
				},
			},
			AppConfig: config.AppConfig{
				GroundControlURL: config.URL(srv.URL),
				UseUnsecure:      true,
			},
		},
	)
	require.NoError(t, err)
	ctx, log, logs := logtest.Capture(t)
	p := newTestRefreshProcess()
	p.cm = cm
	p.log = log

	require.NoError(t, p.Execute(ctx))
	require.Equal(t, refreshedSecret, cm.GetSourceRegistryPassword())
	require.NotContains(t, logs.String(), refreshedSecret)
	require.NotContains(t, logs.String(), currentSecret)
}
