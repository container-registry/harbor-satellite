package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func writeTempConfig(t *testing.T, data any) string {
	tempDir := t.TempDir()
	path := filepath.Join(tempDir, "config.json")

	bytes, err := json.Marshal(data)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(path, bytes, 0600))

	return path
}

func TestInitConfigManager(t *testing.T) {
	validConfig := Config{
		AppConfig: AppConfig{
			GroundControlURL: "http://localhost",
			LogLevel:         "info",
		},
		ZotConfigRaw: json.RawMessage(`{"storage": {}}`),
	}
	validConfigPath := writeTempConfig(t, validConfig)

	invalidConfigPath := filepath.Join(t.TempDir(), "invalid.json")
	fmt.Println(validConfigPath)
	require.NoError(t, os.WriteFile(invalidConfigPath, []byte("not-json"), 0600))

	tests := []struct {
		name    string
		path    string
		wantErr bool
	}{
		{name: "Success", path: validConfigPath, wantErr: false},
		{name: "FileMissing", path: "/non/existent/path.json", wantErr: false}, // missing file uses defaults
		{name: "InvalidJSON", path: invalidConfigPath, wantErr: true},
	}

	token := "dummy-token"
	ground_control_url := "http://groundcontrol"

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, _, err := InitConfigManager(token, ground_control_url, tt.path, "", false, false)
			if tt.wantErr {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestConfigManager_WriteConfig(t *testing.T) {
	cfg := &Config{
		AppConfig: AppConfig{
			LogLevel: "info",
		},
		ZotConfigRaw: json.RawMessage(`{"storage": {}}`),
	}
	path := filepath.Join(t.TempDir(), "config.json")
	cm, err := NewConfigManager(path, "", "", "", false, cfg)
	require.NoError(t, err)

	t.Run("SuccessfulWrite", func(t *testing.T) {
		cm.With(func(c *Config) {
			c.AppConfig.LogLevel = "warn"
		})
		require.NoError(t, cm.WriteConfig())

		data, err := os.ReadFile(path) //nolint:gosec // G304: test file path
		require.NoError(t, err)

		var saved Config
		require.NoError(t, json.Unmarshal(data, &saved))
		require.Equal(t, "warn", saved.AppConfig.LogLevel)
	})
}
