package config

import (
	"encoding/json"
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
				require.NoError(t, os.MkdirAll(dir, 0755))
				return dir
			},
			expectErr: false,
		},
		{
			name: "Path is existing file",
			setup: func(t *testing.T) string {
				f := filepath.Join(t.TempDir(), "file")
				require.NoError(t, os.WriteFile(f, []byte("data"), 0600))
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

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
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
			require.Equal(t, filepath.Join(pathConfig.ConfigDir, "zot-hot.json"), pathConfig.ZotTempConfig)
			require.Empty(t, pathConfig.ZotStorageDir, "ZotStorageDir is set by ResolveRegistryDataDir, not ResolvePathConfig")
		})
	}
}

func TestDefaultRegistryDataDir(t *testing.T) {
	origGeteuid := geteuid
	t.Cleanup(func() { geteuid = origGeteuid })

	home, err := os.UserHomeDir()
	require.NoError(t, err)

	t.Run("root returns /var/lib path", func(t *testing.T) {
		geteuid = func() int { return 0 }
		t.Setenv("XDG_DATA_HOME", "/should/be/ignored")

		got, err := DefaultRegistryDataDir()
		require.NoError(t, err)
		require.Equal(t, "/var/lib/satellite/registry", got)
	})

	t.Run("non-root with XDG_DATA_HOME set", func(t *testing.T) {
		geteuid = func() int { return 1000 }
		t.Setenv("XDG_DATA_HOME", "/tmp/xdg-data")

		got, err := DefaultRegistryDataDir()
		require.NoError(t, err)
		require.Equal(t, filepath.Join("/tmp/xdg-data", "satellite", "registry"), got)
	})

	t.Run("non-root without XDG_DATA_HOME falls back to ~/.local/share", func(t *testing.T) {
		geteuid = func() int { return 1000 }
		t.Setenv("XDG_DATA_HOME", "")

		got, err := DefaultRegistryDataDir()
		require.NoError(t, err)
		require.Equal(t, filepath.Join(home, ".local", "share", "satellite", "registry"), got)
	})
}

func TestResolveRegistryDataDir(t *testing.T) {
	origGeteuid := geteuid
	t.Cleanup(func() { geteuid = origGeteuid })
	geteuid = func() int { return 1000 }

	t.Run("override is used and created", func(t *testing.T) {
		override := filepath.Join(t.TempDir(), "override-registry")

		got, err := ResolveRegistryDataDir(override)
		require.NoError(t, err)
		require.Equal(t, override, got)
		require.DirExists(t, override)
	})

	t.Run("empty override falls back to default and creates it", func(t *testing.T) {
		xdg := t.TempDir()
		t.Setenv("XDG_DATA_HOME", xdg)

		got, err := ResolveRegistryDataDir("")
		require.NoError(t, err)
		require.Equal(t, filepath.Join(xdg, "satellite", "registry"), got)
		require.DirExists(t, got)
	})

	t.Run("tilde override is expanded", func(t *testing.T) {
		home := t.TempDir()
		t.Setenv("HOME", home)

		got, err := ResolveRegistryDataDir("~/data/zot")
		require.NoError(t, err)
		require.Equal(t, filepath.Join(home, "data", "zot"), got)
		require.DirExists(t, got)
	})

	t.Run("non-writable override returns error", func(t *testing.T) {
		if os.Geteuid() == 0 {
			t.Skip("root bypasses chmod permissions")
		}

		base := t.TempDir()
		require.NoError(t, os.Chmod(base, 0500))
		t.Cleanup(func() { _ = os.Chmod(base, 0700) })

		_, err := ResolveRegistryDataDir(filepath.Join(base, "child"))
		require.Error(t, err)
	})
}

func TestBuildZotConfigWithStoragePath(t *testing.T) {
	storagePath := "/custom/zot/storage"

	result, err := BuildZotConfigWithStoragePath(storagePath)
	require.NoError(t, err)
	require.NotEmpty(t, result)

	var parsed map[string]any
	require.NoError(t, json.Unmarshal([]byte(result), &parsed), "output should be valid JSON")

	storage, ok := parsed["storage"].(map[string]any)
	require.True(t, ok, "storage section should exist")
	require.Equal(t, storagePath, storage["rootDirectory"])
}
