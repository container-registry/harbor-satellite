package config

import (
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
				return filepath.Join(os.TempDir(), "test-satellite-new")
			},
			expectErr: false,
		},
		{
			name: "Existing directory",
			setup: func(t *testing.T) string {
				dir := filepath.Join(os.TempDir(), "test-satellite-existing")
				require.NoError(t, os.MkdirAll(dir, 0755))
				return dir
			},
			expectErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := tt.setup(t)
			t.Cleanup(func() {
				if err := os.RemoveAll(path); err != nil {
					t.Errorf("cleanup: %v", err)
				}
			})

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
		configDir string
		expectErr bool
	}{
		{
			name:      "Temp directory",
			configDir: filepath.Join(os.TempDir(), "test-satellite-resolve"),
			expectErr: false,
		},
		{
			name:      "Tilde expansion",
			configDir: "~/test-satellite-resolve",
			expectErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pathConfig, err := ResolvePathConfig(tt.configDir)
			if tt.expectErr {
				require.Error(t, err)
				return
			}

			require.NoError(t, err)
			require.NotNil(t, pathConfig)

			t.Cleanup(func() {
				if err := os.RemoveAll(pathConfig.ConfigDir); err != nil {
					t.Errorf("cleanup: %v", err)
				}
			})

			require.DirExists(t, pathConfig.ConfigDir)
			require.Equal(t, filepath.Join(pathConfig.ConfigDir, "config.json"), pathConfig.ConfigFile)
			require.Equal(t, filepath.Join(pathConfig.ConfigDir, "prev_config.json"), pathConfig.PrevConfigFile)
			require.Equal(t, filepath.Join(pathConfig.ConfigDir, "zot-hot.json"), pathConfig.ZotTempConfig)
			require.Equal(t, filepath.Join(pathConfig.ConfigDir, "zot"), pathConfig.ZotStorageDir)
		})
	}
}

func TestBuildZotConfigWithStoragePath(t *testing.T) {
	storagePath := "/custom/zot/storage"

	result, err := BuildZotConfigWithStoragePath(storagePath)
	require.NoError(t, err)
	require.NotEmpty(t, result)
	require.Contains(t, result, storagePath)
	require.Contains(t, result, `"rootDirectory"`)
}
