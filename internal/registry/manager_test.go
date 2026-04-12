package registry

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/require"
)

var validZotConfig = []byte(`{
    "distSpecVersion": "1.1.0",
    "storage": { "rootDirectory": "./zot" },
    "http": { "address": "127.0.0.1", "port": "5000" },
    "log": { "level": "info" }
}`)

func TestWriteTempZotConfig(t *testing.T) {
	log := zerolog.Nop()

	tests := []struct {
		name      string
		config    []byte
		expectErr bool
	}{
		{"Valid JSON", validZotConfig, false},
		{"Empty Config", []byte{}, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tmpPath := filepath.Join(t.TempDir(), "zot-test.json")
			zm := NewZotManager(log, tt.config, tmpPath)
			err := zm.WriteTempZotConfig()
			if tt.expectErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)

			data, readErr := os.ReadFile(tmpPath)
			require.NoError(t, readErr)
			require.Equal(t, tt.config, data)
		})
	}
}

func TestRemoveTempZotConfig(t *testing.T) {
	log := zerolog.Nop()
	tmpPath := filepath.Join(t.TempDir(), "zot-test.json")
	zm := NewZotManager(log, nil, tmpPath)

	tests := []struct {
		name        string
		setup       func(t *testing.T) string
		expectedErr bool
	}{
		{
			name: "File Exists",
			setup: func(t *testing.T) string {
				f := filepath.Join(t.TempDir(), "testfile.json")
				require.NoError(t, os.WriteFile(f, []byte("test"), 0600))
				return f
			},
			expectedErr: false,
		},
		{
			name: "File Does Not Exist",
			setup: func(t *testing.T) string {
				return filepath.Join(t.TempDir(), "nonexistent.json")
			},
			expectedErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := tt.setup(t)

			err := zm.RemoveTempZotConfig(path)

			if tt.expectedErr {
				require.Error(t, err)
			} else {
				require.NoError(t, err)

				_, statErr := os.Stat(path)
				require.True(t, os.IsNotExist(statErr))
			}
		})
	}
}

func TestVerifyRegistryConfig(t *testing.T) {
	log := zerolog.Nop()
	tmpPath := filepath.Join(t.TempDir(), "zot-test.json")
	zm := NewZotManager(log, nil, tmpPath)

	tests := []struct {
		name        string
		configData  []byte
		expectError bool
	}{
		{
			name:        "Valid Config",
			configData:  validZotConfig,
			expectError: false,
		},
		{
			name:        "Invalid Config",
			configData:  []byte(`{invalid-json}`),
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tmpFile := filepath.Join(t.TempDir(), "zot-config.json")
			require.NoError(t, os.WriteFile(tmpFile, tt.configData, 0600))

			err := zm.VerifyRegistryConfig(tmpFile)

			if tt.expectError {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}
		})
	}
}
