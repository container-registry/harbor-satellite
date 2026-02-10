package state

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"sync"
	"testing"

	runtime "github.com/container-registry/harbor-satellite/internal/container_runtime"
	"github.com/container-registry/harbor-satellite/pkg/config"
	"github.com/stretchr/testify/require"
)

func TestFormatCRIActivity(t *testing.T) {
	tests := []struct {
		name    string
		results []runtime.CRIConfigResult
		want    string
	}{
		{
			name: "single success with backup",
			results: []runtime.CRIConfigResult{
				{CRI: runtime.CRIDocker, Success: true, BackupPath: "/etc/docker/daemon.json.bak.20250129T100000"},
			},
			want: "cri_fallback_configured: docker(ok, backup:/etc/docker/daemon.json.bak.20250129T100000)",
		},
		{
			name: "single success without backup",
			results: []runtime.CRIConfigResult{
				{CRI: runtime.CRIContainerd, Success: true},
			},
			want: "cri_fallback_configured: containerd(ok)",
		},
		{
			name: "single failure",
			results: []runtime.CRIConfigResult{
				{CRI: runtime.CRIDocker, Success: false, Error: "failed to restart Docker"},
			},
			want: "cri_fallback_configured: docker(err:failed to restart Docker)",
		},
		{
			name: "mixed results",
			results: []runtime.CRIConfigResult{
				{CRI: runtime.CRIDocker, Success: true, BackupPath: "/etc/docker/daemon.json.bak.20250129T100000"},
				{CRI: runtime.CRIContainerd, Success: true},
				{CRI: runtime.CRICrio, Success: false, Error: "permission denied"},
			},
			want: "cri_fallback_configured: docker(ok, backup:/etc/docker/daemon.json.bak.20250129T100000), containerd(ok), crio(err:permission denied)",
		},
		{
			name:    "empty results",
			results: []runtime.CRIConfigResult{},
			want:    "cri_fallback_configured: ",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := formatCRIActivity(tt.results)
			require.Equal(t, tt.want, got)
		})
	}
}

func TestSetPendingCRIResults(t *testing.T) {
	t.Run("stores results", func(t *testing.T) {
		p := &StatusReportingProcess{
			name: "test",
			mu:   &sync.Mutex{},
		}
		results := []runtime.CRIConfigResult{
			{CRI: runtime.CRIDocker, Success: true},
		}

		p.SetPendingCRIResults(results)

		p.mu.Lock()
		defer p.mu.Unlock()
		require.Len(t, p.pendingCRI, 1)
		require.Equal(t, runtime.CRIDocker, p.pendingCRI[0].CRI)
	})

	t.Run("overwrites previous results", func(t *testing.T) {
		p := &StatusReportingProcess{
			name: "test",
			mu:   &sync.Mutex{},
		}

		p.SetPendingCRIResults([]runtime.CRIConfigResult{
			{CRI: runtime.CRIDocker, Success: true},
		})
		p.SetPendingCRIResults([]runtime.CRIConfigResult{
			{CRI: runtime.CRIContainerd, Success: false, Error: "fail"},
		})

		p.mu.Lock()
		defer p.mu.Unlock()
		require.Len(t, p.pendingCRI, 1)
		require.Equal(t, runtime.CRIContainerd, p.pendingCRI[0].CRI)
	})

	t.Run("nil results clears pending", func(t *testing.T) {
		p := &StatusReportingProcess{
			name: "test",
			mu:   &sync.Mutex{},
		}
		p.SetPendingCRIResults([]runtime.CRIConfigResult{
			{CRI: runtime.CRIDocker, Success: true},
		})
		p.SetPendingCRIResults(nil)

		p.mu.Lock()
		defer p.mu.Unlock()
		require.Nil(t, p.pendingCRI)
	})
}

func newReportingTestCM(t *testing.T, gcURL string) *config.ConfigManager {
	t.Helper()
	dir := t.TempDir()
	cfg := &config.Config{
		StateConfig: config.StateConfig{
			StateURL: "http://registry/satellite/satellite-state/test-sat/state:latest",
		},
		AppConfig: config.AppConfig{
			GroundControlURL:  config.URL(gcURL),
			HeartbeatInterval: "@every 30s",
			UseUnsecure:       true,
		},
		ZotConfigRaw: json.RawMessage(`{}`),
	}
	cm, err := config.NewConfigManager(
		filepath.Join(dir, "config.json"),
		filepath.Join(dir, "prev.json"),
		"token", gcURL, false, cfg,
	)
	require.NoError(t, err)
	return cm
}

func TestExecute_CRIReporting(t *testing.T) {
	criResults := []runtime.CRIConfigResult{
		{CRI: runtime.CRIDocker, Success: true, BackupPath: "/etc/docker/daemon.json.bak"},
	}

	t.Run("successful send clears CRI results", func(t *testing.T) {
		var received StatusReportParams
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			require.NoError(t, json.NewDecoder(r.Body).Decode(&received))
			w.WriteHeader(http.StatusOK)
		}))
		defer srv.Close()

		cm := newReportingTestCM(t, srv.URL)
		p := &StatusReportingProcess{name: "test", mu: &sync.Mutex{}, cm: cm}
		p.SetPendingCRIResults(criResults)

		ctx := testContext()
		err := p.Execute(ctx)
		require.NoError(t, err)

		require.Contains(t, received.Activity, "docker(ok")

		p.mu.Lock()
		require.True(t, p.criReported)
		require.Nil(t, p.pendingCRI)
		p.mu.Unlock()
	})

	t.Run("failed send preserves CRI results", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusInternalServerError)
		}))
		defer srv.Close()

		cm := newReportingTestCM(t, srv.URL)
		p := &StatusReportingProcess{name: "test", mu: &sync.Mutex{}, cm: cm}
		p.SetPendingCRIResults(criResults)

		ctx := testContext()
		err := p.Execute(ctx)
		require.Error(t, err)

		p.mu.Lock()
		require.False(t, p.criReported)
		require.NotNil(t, p.pendingCRI)
		p.mu.Unlock()
	})

	t.Run("second Execute after success skips CRI", func(t *testing.T) {
		var callCount int
		var lastActivity string
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			var req StatusReportParams
			require.NoError(t, json.NewDecoder(r.Body).Decode(&req))
			callCount++
			lastActivity = req.Activity
			w.WriteHeader(http.StatusOK)
		}))
		defer srv.Close()

		cm := newReportingTestCM(t, srv.URL)
		p := &StatusReportingProcess{name: "test", mu: &sync.Mutex{}, cm: cm}
		p.SetPendingCRIResults(criResults)

		ctx := testContext()
		require.NoError(t, p.Execute(ctx))
		require.Equal(t, 1, callCount)
		require.Contains(t, lastActivity, "docker(ok")

		require.NoError(t, p.Execute(ctx))
		require.Equal(t, 2, callCount)
		require.Empty(t, lastActivity)
	})

	t.Run("retry after failure includes CRI", func(t *testing.T) {
		var shouldFail bool = true
		var lastActivity string
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			var req StatusReportParams
			require.NoError(t, json.NewDecoder(r.Body).Decode(&req))
			lastActivity = req.Activity
			if shouldFail {
				w.WriteHeader(http.StatusInternalServerError)
				return
			}
			w.WriteHeader(http.StatusOK)
		}))
		defer srv.Close()

		cm := newReportingTestCM(t, srv.URL)
		p := &StatusReportingProcess{name: "test", mu: &sync.Mutex{}, cm: cm}
		p.SetPendingCRIResults(criResults)

		ctx := testContext()
		err := p.Execute(ctx)
		require.Error(t, err)
		require.Contains(t, lastActivity, "docker(ok")

		p.mu.Lock()
		require.False(t, p.criReported)
		require.NotNil(t, p.pendingCRI)
		p.mu.Unlock()

		shouldFail = false
		err = p.Execute(ctx)
		require.NoError(t, err)
		require.Contains(t, lastActivity, "docker(ok")

		p.mu.Lock()
		require.True(t, p.criReported)
		require.Nil(t, p.pendingCRI)
		p.mu.Unlock()
	})
}
