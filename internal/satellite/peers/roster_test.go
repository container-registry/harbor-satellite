package peers

import (
	"testing"

	"github.com/container-registry/harbor-satellite/pkg/config"
	"github.com/stretchr/testify/require"
)

func TestEligiblePeers(t *testing.T) {
	staticA := config.PeerDescriptor{
		ID:       "satellite-a",
		URL:      "https://satellite-a.example:5000",
		Groups:   []string{"edge-site-a"},
		Username: "operator",
		Password: "secret",
		TLS:      config.TLSConfig{CAFile: "/certs/ca.pem"},
	}
	staticB := config.PeerDescriptor{
		ID:     "satellite-b",
		URL:    "https://satellite-b.example:5000",
		Groups: []string{"edge-site-b"},
	}

	tests := []struct {
		name string
		cfg  config.PeerDistributionConfig
		want []config.PeerDescriptor
	}{
		{
			name: "disabled returns nil",
			cfg: config.PeerDistributionConfig{
				Enabled:     false,
				LocalGroups: []string{"edge-site-a"},
				StaticPeers: []config.PeerDescriptor{staticA},
			},
		},
		{
			name: "empty gc peers keeps intersecting static peers",
			cfg: config.PeerDistributionConfig{
				Enabled:      true,
				LocalGroups:  []string{"edge-site-a"},
				StaticPeers:  []config.PeerDescriptor{staticA, staticB},
				GCPeers:      []config.PeerDescriptor{},
				ReachoutSats: "group",
			},
			want: []config.PeerDescriptor{staticA},
		},
		{
			name: "same id keeps static url and takes gc groups",
			cfg: config.PeerDistributionConfig{
				Enabled:     true,
				LocalGroups: []string{"from-gc"},
				StaticPeers: []config.PeerDescriptor{staticA},
				GCPeers: []config.PeerDescriptor{{
					ID:     "satellite-a",
					URL:    "https://other.example:5000",
					Groups: []string{"from-gc"},
				}},
				ReachoutSats: "group",
			},
			want: []config.PeerDescriptor{{
				ID:       "satellite-a",
				URL:      "https://satellite-a.example:5000",
				Groups:   []string{"from-gc"},
				Username: "operator",
				Password: "secret",
				TLS:      config.TLSConfig{CAFile: "/certs/ca.pem"},
			}},
		},
		{
			name: "same canonical url collapses to the first peer",
			cfg: config.PeerDistributionConfig{
				Enabled:     true,
				LocalGroups: []string{"edge-site-a"},
				StaticPeers: []config.PeerDescriptor{
					staticA,
					{
						ID:     "satellite-a-alias",
						URL:    "https://Satellite-A.example:5000/",
						Groups: []string{"edge-site-a"},
					},
				},
				ReachoutSats: "group",
			},
			want: []config.PeerDescriptor{staticA},
		},
		{
			name: "omitted static peer stays",
			cfg: config.PeerDistributionConfig{
				Enabled:     true,
				LocalGroups: []string{"edge-site-a", "edge-site-b"},
				StaticPeers: []config.PeerDescriptor{staticA, staticB},
				GCPeers: []config.PeerDescriptor{{
					ID:     "satellite-a",
					URL:    "https://satellite-a.example:5000",
					Groups: []string{"edge-site-a"},
				}},
				ReachoutSats: "group",
			},
			want: []config.PeerDescriptor{staticA, staticB},
		},
		{
			name: "gc only peer is appended",
			cfg: config.PeerDistributionConfig{
				Enabled:     true,
				LocalGroups: []string{"edge-site-a", "edge-site-c"},
				StaticPeers: []config.PeerDescriptor{staticA},
				GCPeers: []config.PeerDescriptor{{
					ID:     "satellite-c",
					URL:    "https://satellite-c.example:5000",
					Groups: []string{"edge-site-c"},
				}},
				ReachoutSats: "group",
			},
			want: []config.PeerDescriptor{
				staticA,
				{
					ID:     "satellite-c",
					URL:    "https://satellite-c.example:5000",
					Groups: []string{"edge-site-c"},
				},
			},
		},
		{
			name: "missing local groups drops every peer",
			cfg: config.PeerDistributionConfig{
				Enabled:      true,
				StaticPeers:  []config.PeerDescriptor{staticA},
				ReachoutSats: "group",
			},
		},
		{
			name: "missing peer groups drops that peer",
			cfg: config.PeerDistributionConfig{
				Enabled:     true,
				LocalGroups: []string{"edge-site-a"},
				StaticPeers: []config.PeerDescriptor{{
					ID:  "satellite-a",
					URL: "https://satellite-a.example:5000",
				}},
				ReachoutSats: "group",
			},
		},
		{
			name: "non overlapping groups are dropped",
			cfg: config.PeerDistributionConfig{
				Enabled:      true,
				LocalGroups:  []string{"edge-site-a"},
				StaticPeers:  []config.PeerDescriptor{staticB},
				ReachoutSats: "group",
			},
		},
		{
			name: "global keeps cross group and unknown group peers",
			cfg: config.PeerDistributionConfig{
				Enabled:     true,
				LocalGroups: []string{"edge-site-a"},
				StaticPeers: []config.PeerDescriptor{
					staticB,
					{
						ID:  "satellite-d",
						URL: "https://satellite-d.example:5000",
					},
				},
				ReachoutSats: "global",
			},
			want: []config.PeerDescriptor{
				staticB,
				{
					ID:  "satellite-d",
					URL: "https://satellite-d.example:5000",
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := EligiblePeers(tt.cfg)
			require.Equal(t, tt.want, got)
		})
	}
}

func TestEligiblePeersLeavesInputUnchanged(t *testing.T) {
	staticGroups := []string{"edge-site-a"}
	gcGroups := []string{"from-gc"}
	cfg := config.PeerDistributionConfig{
		Enabled:     true,
		LocalGroups: []string{"from-gc"},
		StaticPeers: []config.PeerDescriptor{{
			ID:     "satellite-a",
			URL:    "https://satellite-a.example:5000",
			Groups: staticGroups,
		}},
		GCPeers: []config.PeerDescriptor{{
			ID:     "satellite-a",
			URL:    "https://other.example:5000",
			Groups: gcGroups,
		}},
		ReachoutSats: "group",
	}

	got := EligiblePeers(cfg)

	require.Equal(t, []string{"edge-site-a"}, cfg.StaticPeers[0].Groups)
	require.Equal(t, []string{"from-gc"}, cfg.GCPeers[0].Groups)
	require.Equal(t, []string{"from-gc"}, got[0].Groups)

	got[0].Groups[0] = "mutated"
	require.Equal(t, []string{"edge-site-a"}, staticGroups)
	require.Equal(t, []string{"from-gc"}, gcGroups)
}

func TestCanonicalPeerURL(t *testing.T) {
	left, ok := canonicalPeerURL("https://User:pass@Satellite-A.example/")
	require.True(t, ok)
	right, ok := canonicalPeerURL("https://satellite-a.example:443")
	require.True(t, ok)
	require.Equal(t, left, right)

	_, ok = canonicalPeerURL("https:peer-a")
	require.False(t, ok)
}
