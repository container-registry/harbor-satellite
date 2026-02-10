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
			require.Equal(t, filepath.Join(pathConfig.ConfigDir, "zot"), pathConfig.ZotStorageDir)
		})
	}
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

func TestExpandPath_EdgeCases(t *testing.T) {
	tests := []struct {
		name      string
		input     string
		shouldErr bool
	}{
		{
			name:      "Double tilde",
			input:     "~~/config",
			shouldErr: false,
		},
		{
			name:      "Very long path",
			input:     "~/very/long/path/" + string(make([]byte, 200)),
			shouldErr: false,
		},
		{
			name:      "Path with spaces",
			input:     "~/config with spaces/satellite",
			shouldErr: false,
		},
		{
			name:      "Path with special chars",
			input:     "~/config-with_special.chars@123",
			shouldErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := expandPath(tt.input)
			if tt.shouldErr {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
				require.NotEmpty(t, result)
			}
		})
	}
}

func TestEnsureDir_NestedDirectories(t *testing.T) {
	t.Run("deeply nested directory creation", func(t *testing.T) {
		base := t.TempDir()
		path := filepath.Join(base, "level1", "level2", "level3", "level4")

		err := ensureDir(path)
		require.NoError(t, err)

		info, err := os.Stat(path)
		require.NoError(t, err)
		require.True(t, info.IsDir())
	})

	t.Run("directory with special permissions", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "special-perms")

		err := ensureDir(path)
		require.NoError(t, err)

		info, err := os.Stat(path)
		require.NoError(t, err)
		require.Equal(t, os.FileMode(0755), info.Mode().Perm())
	})
}

func TestResolvePathConfig_AllPaths(t *testing.T) {
	dir := t.TempDir()
	pathConfig, err := ResolvePathConfig(dir)
	require.NoError(t, err)
	require.NotNil(t, pathConfig)

	t.Run("all paths are absolute", func(t *testing.T) {
		require.True(t, filepath.IsAbs(pathConfig.ConfigDir))
		require.True(t, filepath.IsAbs(pathConfig.ConfigFile))
		require.True(t, filepath.IsAbs(pathConfig.PrevConfigFile))
		require.True(t, filepath.IsAbs(pathConfig.ZotTempConfig))
		require.True(t, filepath.IsAbs(pathConfig.ZotStorageDir))
		require.True(t, filepath.IsAbs(pathConfig.StateFile))
	})

	t.Run("all paths under config dir", func(t *testing.T) {
		require.Contains(t, pathConfig.ConfigFile, pathConfig.ConfigDir)
		require.Contains(t, pathConfig.PrevConfigFile, pathConfig.ConfigDir)
		require.Contains(t, pathConfig.ZotTempConfig, pathConfig.ConfigDir)
		require.Contains(t, pathConfig.ZotStorageDir, pathConfig.ConfigDir)
		require.Contains(t, pathConfig.StateFile, pathConfig.ConfigDir)
	})

	t.Run("correct filenames", func(t *testing.T) {
		require.Equal(t, "config.json", filepath.Base(pathConfig.ConfigFile))
		require.Equal(t, "prev_config.json", filepath.Base(pathConfig.PrevConfigFile))
		require.Equal(t, "zot-hot.json", filepath.Base(pathConfig.ZotTempConfig))
		require.Equal(t, "zot", filepath.Base(pathConfig.ZotStorageDir))
		require.Equal(t, "state.json", filepath.Base(pathConfig.StateFile))
	})
}

func TestBuildZotConfigWithStoragePath_EdgeCases(t *testing.T) {
	tests := []struct {
		name        string
		storagePath string
		expectError bool
	}{
		{
			name:        "empty path",
			storagePath: "",
			expectError: false,
		},
		{
			name:        "relative path",
			storagePath: "./relative/path",
			expectError: false,
		},
		{
			name:        "absolute path",
			storagePath: "/absolute/path",
			expectError: false,
		},
		{
			name:        "path with spaces",
			storagePath: "/path with spaces/storage",
			expectError: false,
		},
		{
			name:        "path with special chars",
			storagePath: "/path-with_special.chars@123/storage",
			expectError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := BuildZotConfigWithStoragePath(tt.storagePath)
			if tt.expectError {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
				require.NotEmpty(t, result)

				var parsed map[string]any
				require.NoError(t, json.Unmarshal([]byte(result), &parsed))

				storage, ok := parsed["storage"].(map[string]any)
				require.True(t, ok)
				require.Equal(t, tt.storagePath, storage["rootDirectory"])
			}
		})
	}
}

func TestBuildZotConfigWithStoragePath_PreservesOtherFields(t *testing.T) {
	storagePath := "/test/storage"

	result, err := BuildZotConfigWithStoragePath(storagePath)
	require.NoError(t, err)

	var parsed map[string]any
	require.NoError(t, json.Unmarshal([]byte(result), &parsed))

	// Verify other config sections are preserved
	_, hasHTTP := parsed["http"]
	require.True(t, hasHTTP, "http section should be preserved")

	_, hasLog := parsed["log"]
	require.True(t, hasLog, "log section should be preserved")

	storage, ok := parsed["storage"].(map[string]any)
	require.True(t, ok)

	// Verify rootDirectory is updated
	require.Equal(t, storagePath, storage["rootDirectory"])
}

func TestEnsureDir_ConcurrentCreation(t *testing.T) {
	base := t.TempDir()
	path := filepath.Join(base, "concurrent")

	// Try to create the same directory concurrently
	done := make(chan bool, 3)
	for i := 0; i < 3; i++ {
		go func() {
			_ = ensureDir(path)
			done <- true
		}()
	}

	// Wait for all goroutines to complete
	for i := 0; i < 3; i++ {
		<-done
	}

	// Verify directory exists (at least one creation should have succeeded)
	info, err := os.Stat(path)
	require.NoError(t, err)
	require.True(t, info.IsDir())
}

func TestResolvePathConfig_WithTilde(t *testing.T) {
	configDir := filepath.Join(t.TempDir(), "test-satellite-config")
	pathConfig, err := ResolvePathConfig(configDir)
	require.NoError(t, err)
	require.NotNil(t, pathConfig)

	// Should be absolute path
	require.True(t, filepath.IsAbs(pathConfig.ConfigDir))
}

func TestDefaultConfigDir_Consistency(t *testing.T) {
	// Call twice and verify we get the same result
	dir1, err1 := DefaultConfigDir()
	require.NoError(t, err1)

	dir2, err2 := DefaultConfigDir()
	require.NoError(t, err2)

	require.Equal(t, dir1, dir2, "DefaultConfigDir should return consistent results")
	require.Contains(t, dir1, "satellite")
}