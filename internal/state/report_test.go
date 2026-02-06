package state

import (
	"testing"
	"time"

	"github.com/container-registry/harbor-satellite/pkg/config"
	"github.com/stretchr/testify/require"
)

func TestCollectStatusReportParams_EmptyRegistryURL(t *testing.T) {
	ctx := testContext()
	req := &StatusReportParams{}
	cfg := config.MetricsConfig{}

	collectStatusReportParams(ctx, 30*time.Second, req, cfg, "", false)

	require.Nil(t, req.CachedImages)
	require.Equal(t, 0, req.ImageCount)
}

func TestCollectStatusReportParams_UnreachableRegistry(t *testing.T) {
	ctx := testContext()
	req := &StatusReportParams{}
	cfg := config.MetricsConfig{}

	collectStatusReportParams(ctx, 30*time.Second, req, cfg, "127.0.0.1:1", true)

	// Should gracefully handle the error - no cached images, image count stays 0
	require.Nil(t, req.CachedImages)
	require.Equal(t, 0, req.ImageCount)
}

func TestExtractSatelliteNameFromURL(t *testing.T) {
	tests := []struct {
		name    string
		url     string
		want    string
		wantErr bool
	}{
		{
			name: "valid URL",
			url:  "https://registry.example.com/satellite/satellite-state/edge-01/state:latest",
			want: "edge-01",
		},
		{
			name: "valid URL with port",
			url:  "http://localhost:8080/satellite/satellite-state/my-sat/state:latest",
			want: "my-sat",
		},
		{
			name:    "missing satellite-state segment",
			url:     "https://registry.example.com/satellite/other/edge-01/state:latest",
			wantErr: true,
		},
		{
			name:    "empty URL",
			url:     "",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := extractSatelliteNameFromURL(tt.url)
			if tt.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			require.Equal(t, tt.want, got)
		})
	}
}

func TestParseEveryExpr(t *testing.T) {
	tests := []struct {
		name     string
		expr     string
		wantDur  time.Duration
		wantErr  bool
	}{
		{
			name:    "valid 30s",
			expr:    "@every 30s",
			wantDur: 30 * time.Second,
		},
		{
			name:    "valid complex",
			expr:    "@every 00h01m30s",
			wantDur: 90 * time.Second,
		},
		{
			name:    "empty",
			expr:    "",
			wantErr: true,
		},
		{
			name:    "missing prefix",
			expr:    "30s",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			d, err := parseEveryExpr(tt.expr)
			if tt.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			require.Equal(t, tt.wantDur, d)
		})
	}
}
