package utils

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestConstructHarborDeleteURL_valid(t *testing.T) {
	tests := []struct {
		name      string
		repo      string
		repoType  string
		wantInURL string
	}{
		{
			name:      "satellite type",
			repo:      "edge-tokyo-01",
			repoType:  "satellite",
			wantInURL: "satellite-state%252Fedge-tokyo-01%252Fstate",
		},
		{
			name:      "group type",
			repo:      "prod-group",
			repoType:  "group",
			wantInURL: "group-state%252Fprod-group%252Fstate",
		},
		{
			name:      "config type",
			repo:      "default-config",
			repoType:  "config",
			wantInURL: "config-state%252Fdefault-config%252Fstate",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ConstructHarborDeleteURL(tt.repo, tt.repoType)
			require.NoError(t, err)
			require.Contains(t, got, "/api/v2.0/projects/satellite/repositories/")
			require.Contains(t, got, tt.wantInURL)
		})
	}
}

func TestConstructHarborDeleteURL_errors(t *testing.T) {
	tests := []struct {
		name     string
		repo     string
		repoType string
	}{
		{name: "empty repo", repo: "", repoType: "satellite"},
		{name: "invalid repoType", repo: "some-repo", repoType: "unknown"},
		{name: "empty repoType", repo: "some-repo", repoType: ""},
		{name: "both empty", repo: "", repoType: ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ConstructHarborDeleteURL(tt.repo, tt.repoType)
			require.Error(t, err)
			require.Empty(t, got)
		})
	}
}
