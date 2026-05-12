package utils

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestConstructHarborDeleteURL(t *testing.T) {
	tests := []struct {
		name      string
		repo      string
		repoType  string
		wantErr   bool
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
		{
			name:     "empty repo",
			repo:     "",
			repoType: "satellite",
			wantErr:  true,
		},
		{
			name:     "invalid repoType",
			repo:     "some-repo",
			repoType: "unknown",
			wantErr:  true,
		},
		{
			name:     "empty repoType",
			repo:     "some-repo",
			repoType: "",
			wantErr:  true,
		},
		{
			name:     "both empty",
			repo:     "",
			repoType: "",
			wantErr:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ConstructHarborDeleteURL(tt.repo, tt.repoType)
			if tt.wantErr {
				require.Error(t, err)
				require.Empty(t, got)
				return
			}
			require.NoError(t, err)
			require.Contains(t, got, "/api/v2.0/projects/satellite/repositories/")
			require.Contains(t, got, tt.wantInURL)
		})
	}
}
