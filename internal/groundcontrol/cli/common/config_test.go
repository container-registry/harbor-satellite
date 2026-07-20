package common

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/spf13/viper"
	"github.com/stretchr/testify/require"
)

func TestStoredTokenRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "credentials.json")
	serverURL := "https://localhost:8080/"
	writer := testRuntime(path, serverURL)
	require.NoError(t, writer.StoreToken("admin", "session-token", time.Now().Add(time.Hour)))

	reader := testRuntime(path, serverURL)
	require.NoError(t, reader.loadStoredToken())
	require.Equal(t, "session-token", reader.config.GetString(tokenKey))

	if runtime.GOOS != "windows" {
		info, err := os.Stat(path)
		require.NoError(t, err)
		require.Equal(t, os.FileMode(0o600), info.Mode().Perm())
	}

	require.NoError(t, reader.RemoveStoredToken())
	removed := testRuntime(path, serverURL)
	require.NoError(t, removed.loadStoredToken())
	require.Empty(t, removed.config.GetString(tokenKey))
}

func TestStoredTokenIsScopedToServer(t *testing.T) {
	path := filepath.Join(t.TempDir(), "credentials.json")
	require.NoError(t, testRuntime(path, "https://one.example").StoreToken(
		"admin",
		"server-one-token",
		time.Now().Add(time.Hour),
	))
	require.NoError(t, testRuntime(path, "https://two.example").StoreToken(
		"admin",
		"server-two-token",
		time.Now().Add(time.Hour),
	))

	serverOne := testRuntime(path, "https://one.example")
	require.NoError(t, serverOne.loadStoredToken())
	require.Equal(t, "server-one-token", serverOne.config.GetString(tokenKey))
	serverTwo := testRuntime(path, "https://two.example")
	require.NoError(t, serverTwo.loadStoredToken())
	require.Equal(t, "server-two-token", serverTwo.config.GetString(tokenKey))

	require.NoError(t, serverOne.RemoveStoredToken())
	serverTwo = testRuntime(path, "https://two.example")
	require.NoError(t, serverTwo.loadStoredToken())
	require.Equal(t, "server-two-token", serverTwo.config.GetString(tokenKey))
}

func testRuntime(credentialsPath string, serverURL string) *Runtime {
	configuration := viper.New()
	configuration.Set(credentialsFileKey, credentialsPath)
	configuration.Set(urlKey, serverURL)
	return &Runtime{config: configuration}
}
