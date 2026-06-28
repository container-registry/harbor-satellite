package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/container-registry/harbor-satellite/internal/crypto"
	"github.com/stretchr/testify/require"
)

func TestConfigManager_detectChangesAudit(t *testing.T) {
	cm := &ConfigManager{}
	base := func() *Config {
		c := &Config{}
		c.AppConfig.Audit = AuditConfig{
			Enabled: true,
			Syslog:  SyslogAudit{Target: "file", File: SyslogAuditFile{Path: "/a.log"}},
		}
		return c
	}

	t.Run("identical audit config yields no change", func(t *testing.T) {
		require.Empty(t, cm.detectChanges(base(), base()))
	})

	t.Run("toggling enabled is detected as an audit change", func(t *testing.T) {
		next := base()
		next.AppConfig.Audit.Enabled = false
		changes := cm.detectChanges(base(), next)
		require.Len(t, changes, 1)
		require.Equal(t, AuditConfigChanged, changes[0].Type)
	})

	t.Run("changing the syslog file path is detected as an audit change", func(t *testing.T) {
		next := base()
		next.AppConfig.Audit.Syslog.File.Path = "/b.log"
		changes := cm.detectChanges(base(), next)
		require.Len(t, changes, 1)
		require.Equal(t, AuditConfigChanged, changes[0].Type)
	})

	t.Run("changing the syslog target is detected as an audit change", func(t *testing.T) {
		next := base()
		next.AppConfig.Audit.Syslog.Target = "daemon"
		changes := cm.detectChanges(base(), next)
		require.Len(t, changes, 1)
		require.Equal(t, AuditConfigChanged, changes[0].Type)
	})
}

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
			_, _, err := InitConfigManager(token, ground_control_url, tt.path, "", false, false, crypto.NewAESProvider())
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
	cm, err := NewConfigManager(path, "", "", "", false, cfg, crypto.NewAESProvider())
	require.NoError(t, err)

	t.Run("SuccessfulWrite", func(t *testing.T) {
		cm.With(func(c *Config) {
			c.AppConfig.LogLevel = "warn"
		})
		require.NoError(t, cm.WriteConfig())

		data, err := os.ReadFile(filepath.Clean(path))
		require.NoError(t, err)

		var saved Config
		require.NoError(t, json.Unmarshal(data, &saved))
		require.Equal(t, "warn", saved.AppConfig.LogLevel)
	})
}
