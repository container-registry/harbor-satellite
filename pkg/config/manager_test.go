package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"

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
	require.NoError(t, os.WriteFile(path, bytes, 0o600))

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
	require.NoError(t, os.WriteFile(invalidConfigPath, []byte("not-json"), 0o600))

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
			_, _, err := InitConfigManager(token, ground_control_url, tt.path, "", false, false, false)
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

		data, err := os.ReadFile(filepath.Clean(path))
		require.NoError(t, err)

		var saved Config
		require.NoError(t, json.Unmarshal(data, &saved))
		require.Equal(t, "warn", saved.AppConfig.LogLevel)
	})
}

// TestGCSkipTLSVerify_IsPerInvocation covers the per-invocation contract of
// --gc-skip-tls-verify. The flag must never be persisted: the config is written
// to disk at startup and rewritten on every reload, so storing it would leave
// certificate verification disabled on later runs that did not pass the flag,
// with nothing on the command line to indicate it.
func TestGCSkipTLSVerify_IsPerInvocation(t *testing.T) {
	const remote = `{"app_config":{"ground_control_url":"https://example.com","log_level":"info"}}`

	newRun := func(t *testing.T, cfgPath, prevPath string, skip bool) *ConfigManager {
		t.Helper()
		cm, _, err := InitConfigManager("tok", "https://example.com", cfgPath, prevPath, false, false, skip)
		require.NoError(t, err)
		return cm
	}

	t.Run("flag is not written to disk", func(t *testing.T) {
		dir := t.TempDir()
		cfgPath := filepath.Join(dir, "config.json")
		require.NoError(t, os.WriteFile(cfgPath, []byte(remote), 0o600))

		cm := newRun(t, cfgPath, filepath.Join(dir, "prev.json"), true)
		require.True(t, cm.GroundControlSkipTLSVerify(), "override applies in-memory for this run")

		// main.go persists the config at startup.
		require.NoError(t, cm.WriteConfig())

		data, err := os.ReadFile(cfgPath)
		require.NoError(t, err)

		var saved Config
		require.NoError(t, json.Unmarshal(data, &saved))
		require.False(t, saved.AppConfig.GroundControlSkipTLSVerify,
			"the CLI opt-in must not be persisted into config.json")
	})

	t.Run("dropping the flag restores verification", func(t *testing.T) {
		dir := t.TempDir()
		cfgPath := filepath.Join(dir, "config.json")
		prevPath := filepath.Join(dir, "prev.json")
		require.NoError(t, os.WriteFile(cfgPath, []byte(remote), 0o600))

		first := newRun(t, cfgPath, prevPath, true)
		require.NoError(t, first.WriteConfig())
		require.True(t, first.GroundControlSkipTLSVerify())

		second := newRun(t, cfgPath, prevPath, false)
		require.False(t, second.GroundControlSkipTLSVerify(),
			"a run without --gc-skip-tls-verify must verify certificates again")
	})

	t.Run("override survives reload without being persisted", func(t *testing.T) {
		dir := t.TempDir()
		cfgPath := filepath.Join(dir, "config.json")
		require.NoError(t, os.WriteFile(cfgPath, []byte(remote), 0o600))

		cm := newRun(t, cfgPath, filepath.Join(dir, "prev.json"), true)

		// Ground Control pushes fresh config and the satellite reloads it.
		require.NoError(t, os.WriteFile(cfgPath, []byte(remote), 0o600))
		_, _, err := cm.ReloadConfig()
		require.NoError(t, err)

		require.True(t, cm.GroundControlSkipTLSVerify(), "override must survive reconciliation")
		require.False(t, cm.GetConfig().AppConfig.GroundControlSkipTLSVerify,
			"surviving a reload must not mean it was written into the config")
	})

	t.Run("config file setting is still honoured", func(t *testing.T) {
		dir := t.TempDir()
		cfgPath := filepath.Join(dir, "config.json")
		require.NoError(t, os.WriteFile(cfgPath,
			[]byte(`{"app_config":{"ground_control_url":"https://example.com","log_level":"info","ground_control_skip_tls_verify":true}}`), 0o600))

		cm := newRun(t, cfgPath, filepath.Join(dir, "prev.json"), false)
		require.True(t, cm.GroundControlSkipTLSVerify(),
			"an explicit config-file opt-in remains a separate, deliberate choice")
	})
}
