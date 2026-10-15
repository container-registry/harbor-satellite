package harbor

import (
	"testing"

	"github.com/goharbor/go-client/pkg/harbor"
	"github.com/stretchr/testify/require"
)

func TestGetClientByConfig_ErrorOnInvalidConfig(t *testing.T) {
	// Provide an invalid URL that fails parsing in NewClientSet
	invalidConfig := &harbor.ClientSetConfig{
		URL:      "://invalid-url",
		Username: "admin",
		Password: "password",
	}

	client, err := GetClientByConfig(invalidConfig)
	require.Error(t, err, "expected error on invalid config URL")
	require.Nil(t, client, "expected client to be nil on error")
}
