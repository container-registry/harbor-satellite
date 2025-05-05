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
    "http": { "address": "127.0.0.1", "port": "8585" },
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
			zm := NewZotManager(&log, tt.config)
			tmpPath, err := zm.WriteTempZotConfig()
			if tt.expectErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)

			data, readErr := os.ReadFile(tmpPath)
			require.NoError(t, readErr)
			require.Equal(t, tt.config, data)
			require.NoError(t, os.Remove(tmpPath))
		})
	}
}

func TestRemoveTempZotConfig(t *testing.T) {
	log := zerolog.Nop()
	zm := NewZotManager(&log, nil)

	tests := []struct {
		name        string
		setup       func() string
		expectedErr bool
	}{
		{
			name: "File Exists",
			setup: func() string {
				tmpFile, err := os.CreateTemp("", "testfile-*.json")
				defer require.NoError(t, tmpFile.Close())
				require.NoError(t, err)
				path := tmpFile.Name()
				return path
			},
			expectedErr: false,
		},
		{
			name: "File Does Not Exist",
			setup: func() string {
				return filepath.Join(os.TempDir(), "nonexistent.json")
			},
			expectedErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := tt.setup()

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
	zm := NewZotManager(&log, nil)

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
			tmpFile, err := os.CreateTemp("", "zot-config-*.json")
			require.NoError(t, err)

			_, writeErr := tmpFile.Write(tt.configData)
			require.NoError(t, writeErr)

			require.NoError(t, tmpFile.Close())

			err = zm.VerifyRegistryConfig(tmpFile.Name())

			if tt.expectError {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}
			require.NoError(t, os.Remove(tmpFile.Name()))
		})
	}
}
