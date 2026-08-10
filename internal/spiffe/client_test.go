//go:build !nospiffe

package spiffe

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestNewClient(t *testing.T) {
	tests := []struct {
		name    string
		cfg     Config
		wantErr string
	}{
		{
			name: "SPIFFE disabled",
			cfg: Config{
				Enabled: false,
			},
			wantErr: "SPIFFE is not enabled",
		},
		{
			name: "SPIFFE enabled, empty ExpectedServerID",
			cfg: Config{
				Enabled:          true,
				ExpectedServerID: "",
			},
			wantErr: "expected server ID must be configured when SPIFFE is enabled",
		},
		{
			name: "SPIFFE enabled, invalid ExpectedServerID",
			cfg: Config{
				Enabled:          true,
				ExpectedServerID: "not-a-valid-spiffe-id",
			},
			wantErr: "invalid expected server ID",
		},
		{
			name: "SPIFFE enabled, valid ExpectedServerID",
			cfg: Config{
				Enabled:          true,
				ExpectedServerID: "spiffe://example.org/gc/main",
			},
			wantErr: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client, err := NewClient(tt.cfg)
			if tt.wantErr != "" {
				require.Error(t, err)
				require.Contains(t, err.Error(), tt.wantErr)
				require.Nil(t, client)
			} else {
				require.NoError(t, err)
				require.NotNil(t, client)
				require.Equal(t, tt.cfg.EndpointSocket, client.socketPath)
				require.Equal(t, tt.cfg.ExpectedServerID, client.expectedServerID.String())
			}
		})
	}
}
