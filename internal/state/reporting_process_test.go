package state

import (
	"sync"
	"testing"

	runtime "github.com/container-registry/harbor-satellite/internal/container_runtime"
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
