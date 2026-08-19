//go:build !nospiffe

package config

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/container-registry/harbor-satellite/internal/crypto"
	"github.com/container-registry/harbor-satellite/internal/satellite/identity"
	"github.com/container-registry/harbor-satellite/internal/satellite/secure"
	"github.com/stretchr/testify/require"
)

// useMockDeviceIdentity swaps the manager's encryptor for one keyed off a mock
// device identity. The real one reads Linux machine identifiers, so key
// derivation fails on other platforms and the test would only run on Linux.
func useMockDeviceIdentity(cm *ConfigManager) {
	cm.encryptor = secure.NewConfigEncryptor(crypto.NewDefaultProvider(), identity.NewMockDeviceIdentity())
}

// writeConfigUnlocked used to decide whether to encrypt from a flag cached when
// the manager was constructed. ReloadConfig replaces cm.config without touching
// that flag, so a reload that turned encrypt_config on still wrote plaintext:
// the operator enabled encryption, the satellite reported no error, and the
// credentials stayed readable on disk. The decision now follows the config
// being written.
func TestConfigManager_ReloadEnablingEncryptionIsHonoured(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "config.json")

	const plaintextCfg = `{"app_config":{"ground_control_url":"https://example.com","log_level":"info","encrypt_config":false}}`
	const encryptedCfg = `{"app_config":{"ground_control_url":"https://example.com","log_level":"info","encrypt_config":true}}`

	require.NoError(t, os.WriteFile(configPath, []byte(plaintextCfg), 0o600))

	cm, _, err := InitConfigManager("tok", "https://example.com", configPath, filepath.Join(dir, "prev.json"), false, false)
	require.NoError(t, err)
	useMockDeviceIdentity(cm)

	// Baseline: encryption off, so the write is plaintext.
	require.NoError(t, cm.WriteConfig())
	data, err := os.ReadFile(filepath.Clean(configPath))
	require.NoError(t, err)
	require.False(t, secure.IsEncrypted(data), "encrypt_config is false, so this write must be plaintext")

	// Ground Control turns encryption on and the satellite reloads.
	require.NoError(t, os.WriteFile(configPath, []byte(encryptedCfg), 0o600))
	_, _, err = cm.ReloadConfig()
	require.NoError(t, err)
	require.True(t, cm.GetConfig().AppConfig.EncryptConfig, "the reloaded config has encryption enabled")

	require.NoError(t, cm.WriteConfig())

	data, err = os.ReadFile(filepath.Clean(configPath))
	require.NoError(t, err)
	require.True(t, secure.IsEncrypted(data),
		"a write after a reload that enabled encryption must actually be encrypted")
	require.NotContains(t, string(data), "ground_control_url",
		"an encrypted config must not leave its fields readable on disk")
}

// WriteConfigToDisk and WritePrevConfigToDisk take a config argument that can
// differ from cm.config, so the encrypt decision has to follow the argument
// rather than the manager's own state.
func TestConfigManager_WriteConfigToDiskFollowsItsArgument(t *testing.T) {
	dir := t.TempDir()

	// Manager built from a config with encryption off.
	cm, err := NewConfigManager(
		filepath.Join(dir, "config.json"), filepath.Join(dir, "prev.json"), "tok", "https://example.com", false,
		&Config{AppConfig: AppConfig{GroundControlURL: URL("https://example.com"), EncryptConfig: false}},
	)
	require.NoError(t, err)
	useMockDeviceIdentity(cm)

	// Asked to write a different config that does want encryption.
	require.NoError(t, cm.WriteConfigToDisk(&Config{
		AppConfig: AppConfig{GroundControlURL: URL("https://example.com"), EncryptConfig: true},
	}))

	data, err := os.ReadFile(filepath.Clean(filepath.Join(dir, "config.json")))
	require.NoError(t, err)
	require.True(t, secure.IsEncrypted(data),
		"the config passed in asked to be encrypted, so it must be written encrypted")
}
