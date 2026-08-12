//go:build nospiffe

package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/container-registry/harbor-satellite/internal/crypto"
	"github.com/stretchr/testify/require"
)

// A build without encryption support must refuse to write a config that the
// operator asked to have encrypted, rather than quietly writing readable
// credentials under an encrypted-looking header.
func TestConfigManager_WriteConfigRefusesWhenEncryptionUnavailable(t *testing.T) {
	cfg := &Config{
		AppConfig: AppConfig{
			LogLevel:      "info",
			EncryptConfig: true,
		},
		ZotConfigRaw: json.RawMessage(`{"storage": {}}`),
	}

	path := filepath.Join(t.TempDir(), "config.json")
	cm, err := NewConfigManager(path, "", "", "", false, cfg)
	require.NoError(t, err)

	err = cm.WriteConfig()
	require.Error(t, err, "encrypt_config is set but this build cannot encrypt")
	require.ErrorIs(t, err, crypto.ErrCryptoUnavailable)

	_, statErr := os.Stat(path)
	require.True(t, os.IsNotExist(statErr), "nothing may be written when the config could not be encrypted")
}

// With encryption switched off the caller has accepted a plaintext config, so
// writing must still work in this build.
func TestConfigManager_WriteConfigPlaintextStillWorks(t *testing.T) {
	cfg := &Config{
		AppConfig: AppConfig{
			LogLevel:      "info",
			EncryptConfig: false,
		},
		ZotConfigRaw: json.RawMessage(`{"storage": {}}`),
	}

	path := filepath.Join(t.TempDir(), "config.json")
	cm, err := NewConfigManager(path, "", "", "", false, cfg)
	require.NoError(t, err)

	require.NoError(t, cm.WriteConfig())

	data, err := os.ReadFile(filepath.Clean(path))
	require.NoError(t, err)

	var saved Config
	require.NoError(t, json.Unmarshal(data, &saved))
	require.Equal(t, "info", saved.AppConfig.LogLevel)
}
