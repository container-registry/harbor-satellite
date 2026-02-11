//go:build !nospiffe

package spiffe

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestAgentInfo_Fields(t *testing.T) {
	info := AgentInfo{
		SpiffeID:        "spiffe://example.com/agent/edge-01",
		AttestationType: "x509pop",
		Selectors:       []string{"x509pop:subject:cn:edge-01"},
		ExpiresAt:       time.Now().Add(time.Hour),
	}

	require.Equal(t, "spiffe://example.com/agent/edge-01", info.SpiffeID)
	require.Equal(t, "x509pop", info.AttestationType)
	require.Len(t, info.Selectors, 1)
	require.Equal(t, "x509pop:subject:cn:edge-01", info.Selectors[0])
	require.False(t, info.ExpiresAt.IsZero())
}

func TestExtractPath(t *testing.T) {
	tests := []struct {
		name        string
		spiffeID    string
		trustDomain string
		expected    string
	}{
		{
			name:        "full spiffe ID",
			spiffeID:    "spiffe://example.com/agent/edge-01",
			trustDomain: "example.com",
			expected:    "/agent/edge-01",
		},
		{
			name:        "already a path",
			spiffeID:    "/agent/edge-01",
			trustDomain: "example.com",
			expected:    "/agent/edge-01",
		},
		{
			name:        "path without leading slash",
			spiffeID:    "agent/edge-01",
			trustDomain: "example.com",
			expected:    "/agent/edge-01",
		},
		{
			name:        "different trust domain adds leading slash",
			spiffeID:    "spiffe://other.com/agent/edge-01",
			trustDomain: "example.com",
			expected:    "/spiffe://other.com/agent/edge-01",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := extractPath(tt.spiffeID, tt.trustDomain)
			require.Equal(t, tt.expected, result)
		})
	}
}
