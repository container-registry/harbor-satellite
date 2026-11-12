package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

type envelopeEncryptor struct{}

type envelope struct {
	Salt []byte `json:"salt"`
	Data []byte `json:"data"`
}

func (envelopeEncryptor) EncryptConfig(cfg any) ([]byte, error) {
	plain, err := json.Marshal(cfg)
	if err != nil {
		return nil, err
	}
	return json.Marshal(envelope{Salt: []byte("s"), Data: plain})
}

func (envelopeEncryptor) DecryptConfig(data []byte, cfg any) error {
	var env envelope
	if err := json.Unmarshal(data, &env); err != nil {
		return err
	}
	return json.Unmarshal(env.Data, cfg)
}

func (envelopeEncryptor) IsEncrypted(data []byte) bool {
	var env envelope
	return json.Unmarshal(data, &env) == nil && len(env.Data) > 0
}

func TestConfigManager_EncryptionNeedsInjectedEncryptor(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	cfg := &Config{AppConfig: AppConfig{LogLevel: "info", EncryptConfig: true}}

	cm, err := NewConfigManager(path, "", "", "", false, cfg)
	require.NoError(t, err)
	require.ErrorIs(t, cm.WriteConfig(), ErrNoEncryptor)
	_, statErr := os.Stat(path)
	require.ErrorIs(t, statErr, os.ErrNotExist, "nothing may be written in plaintext when encryption is on")
}

func TestConfigManager_EncryptedRoundTripThroughInit(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	cfg := &Config{AppConfig: AppConfig{LogLevel: "warn", EncryptConfig: true}}

	cm, err := NewConfigManager(path, "", "", "", false, cfg, WithEncryptor(envelopeEncryptor{}))
	require.NoError(t, err)
	require.NoError(t, cm.WriteConfig())

	raw, err := os.ReadFile(filepath.Clean(path))
	require.NoError(t, err)
	require.NotContains(t, string(raw), "app_config")

	_, _, err = InitConfigManager("", "http://gc.example", path, "", false, false)
	require.ErrorIs(t, err, ErrNoEncryptor, "a sealed config read without an encryptor must not become an empty config")

	loaded, _, err := InitConfigManager("", "http://gc.example", path, "", false, false, WithEncryptor(envelopeEncryptor{}))
	require.NoError(t, err)
	require.Equal(t, "warn", loaded.GetLogLevel())
}
