package config

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestExpandPath(t *testing.T) {
	home, err := os.UserHomeDir()
	require.NoError(t, err)

	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "Tilde expansion",
			input:    "~/config/satellite",
			expected: filepath.Join(home, "config/satellite"),
		},
		{
			name:     "No tilde",
			input:    "/tmp/satellite",
			expected: "/tmp/satellite",
		},
		{
			name:     "Relative path",
			input:    "config/satellite",
			expected: "config/satellite",
		},
		{
			name:     "Bare tilde",
			input:    "~",
			expected: home,
		},
		{
			name:     "Empty string",
			input:    "",
			expected: "",
		},
		{
			name:     "Tilde in middle not expanded",
			input:    "/some/~/path",
			expected: "/some/~/path",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := expandPath(tt.input)
			require.NoError(t, err)
			require.Equal(t, tt.expected, result)
		})
	}
}

func TestEnsureDir(t *testing.T) {
	tests := []struct {
		name      string
		setup     func(t *testing.T) string
		expectErr bool
	}{
		{
			name: "Create new directory",
			setup: func(t *testing.T) string {
				return filepath.Join(t.TempDir(), "new-dir")
			},
			expectErr: false,
		},
		{
			name: "Existing directory",
			setup: func(t *testing.T) string {
				dir := filepath.Join(t.TempDir(), "existing")
				require.NoError(t, os.MkdirAll(dir, 0o755))
				return dir
			},
			expectErr: false,
		},
		{
			name: "Path is existing file",
			setup: func(t *testing.T) string {
				f := filepath.Join(t.TempDir(), "file")
				require.NoError(t, os.WriteFile(f, []byte("data"), 0o600))
				return f
			},
			expectErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := tt.setup(t)

			err := ensureDir(path)
			if tt.expectErr {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
				info, statErr := os.Stat(path)
				require.NoError(t, statErr)
				require.True(t, info.IsDir())
			}
		})
	}
}

func TestDefaultConfigDir(t *testing.T) {
	dir, err := DefaultConfigDir()
	require.NoError(t, err)
	require.NotEmpty(t, dir)
	require.Contains(t, dir, "satellite")
}

func TestResolvePathConfig(t *testing.T) {
	tests := []struct {
		name      string
		configDir func(t *testing.T) string
		expectErr bool
	}{
		{
			name: "Temp directory",
			configDir: func(t *testing.T) string {
				return filepath.Join(t.TempDir(), "resolve")
			},
			expectErr: false,
		},
		{
			name: "Relative path resolves to absolute",
			configDir: func(t *testing.T) string {
				return filepath.Join(t.TempDir(), "relative")
			},
			expectErr: false,
		},
	}

	if os.Geteuid() == 0 {
		t.Skip("root resolves the store to /var/lib; TestDefaultStoreDir covers that case")
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dataHome := t.TempDir()
			t.Setenv("XDG_DATA_HOME", dataHome)

			dir := tt.configDir(t)
			pathConfig, err := ResolvePathConfig(dir)
			if tt.expectErr {
				require.Error(t, err)
				return
			}

			require.NoError(t, err)
			require.NotNil(t, pathConfig)

			require.True(t, filepath.IsAbs(pathConfig.ConfigDir), "ConfigDir should be absolute")
			require.DirExists(t, pathConfig.ConfigDir)
			require.Equal(t, filepath.Join(pathConfig.ConfigDir, "config.json"), pathConfig.ConfigFile)
			require.Equal(t, filepath.Join(pathConfig.ConfigDir, "prev_config.json"), pathConfig.PrevConfigFile)
			require.Equal(t, filepath.Join(dataHome, "satellite", "oci"), pathConfig.StoreDir)
			require.False(t, pathConfig.StoreDirLegacy)
			require.Equal(t, filepath.Join(pathConfig.ConfigDir, "state.json"), pathConfig.StateFile)
			require.Equal(t, filepath.Join(pathConfig.ConfigDir, DefaultDesiredStateFile), pathConfig.DesiredStateFile)
		})
	}
}

func TestDefaultStoreDir(t *testing.T) {
	tests := []struct {
		name        string
		uid         int
		xdgDataHome string
		oldLayout   bool // <configDir>/oci/index.json exists
		oldEmptyDir bool // <configDir>/oci exists but holds nothing
		newLayout   bool // the new default already holds index.json
		want        func(configDir, home string) string
		wantLegacy  bool
	}{
		{
			name:        "non-root with XDG_DATA_HOME",
			uid:         1000,
			xdgDataHome: "/data",
			want:        func(_, _ string) string { return "/data/satellite/oci" },
		},
		{
			name: "non-root without XDG_DATA_HOME falls back to ~/.local/share",
			uid:  1000,
			want: func(_, home string) string { return filepath.Join(home, ".local", "share", "satellite", "oci") },
		},
		{
			name:        "relative XDG_DATA_HOME is ignored",
			uid:         1000,
			xdgDataHome: "data",
			want:        func(_, home string) string { return filepath.Join(home, ".local", "share", "satellite", "oci") },
		},
		{
			name:        "root uses /var/lib and ignores XDG_DATA_HOME",
			uid:         0,
			xdgDataHome: "/data",
			want:        func(_, _ string) string { return "/var/lib/satellite/oci" },
		},
		{
			name:       "old layout kept when the new default is empty",
			uid:        1000,
			oldLayout:  true,
			want:       func(configDir, _ string) string { return filepath.Join(configDir, "oci") },
			wantLegacy: true,
		},
		{
			name:      "old layout ignored once the new default holds one",
			uid:       1000,
			oldLayout: true,
			newLayout: true,
			want:      func(_, home string) string { return filepath.Join(home, ".local", "share", "satellite", "oci") },
		},
		{
			name:        "empty old dir is ignored",
			uid:         1000,
			oldEmptyDir: true,
			want:        func(_, home string) string { return filepath.Join(home, ".local", "share", "satellite", "oci") },
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			configDir := t.TempDir()
			home := t.TempDir()

			oldDir := filepath.Join(configDir, "oci")
			if tt.oldLayout || tt.oldEmptyDir {
				require.NoError(t, os.MkdirAll(oldDir, 0o755))
			}
			if tt.oldLayout {
				require.NoError(t, os.WriteFile(filepath.Join(oldDir, "index.json"), []byte("{}"), 0o600))
			}
			if tt.newLayout {
				newDir := filepath.Join(home, ".local", "share", "satellite", "oci")
				require.NoError(t, os.MkdirAll(newDir, 0o755))
				require.NoError(t, os.WriteFile(filepath.Join(newDir, "index.json"), []byte("{}"), 0o600))
			}

			homeDir := func() (string, error) { return home, nil }
			dir, legacy, err := defaultStoreDir(configDir, tt.uid, tt.xdgDataHome, homeDir)

			require.NoError(t, err)
			require.Equal(t, tt.want(configDir, home), dir)
			require.Equal(t, tt.wantLegacy, legacy)
		})
	}

	t.Run("home is not needed for root or an absolute XDG_DATA_HOME", func(t *testing.T) {
		noHome := func() (string, error) { return "", errors.New("no home") }

		dir, _, err := defaultStoreDir(t.TempDir(), 0, "", noHome)
		require.NoError(t, err)
		require.Equal(t, "/var/lib/satellite/oci", dir)

		dir, _, err = defaultStoreDir(t.TempDir(), 1000, "/data", noHome)
		require.NoError(t, err)
		require.Equal(t, "/data/satellite/oci", dir)

		_, _, err = defaultStoreDir(t.TempDir(), 1000, "", noHome)
		require.Error(t, err)
	})
}
