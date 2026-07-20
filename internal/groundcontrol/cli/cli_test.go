package cli_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/container-registry/harbor-satellite/internal/groundcontrol/cli"
	"github.com/stretchr/testify/require"
)

func TestGetUsersUsesBearerToken(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		require.Equal(t, "/api/users", request.URL.Path)
		require.Equal(t, "Bearer session-token", request.Header.Get("Authorization"))
		writer.Header().Set("Content-Type", "application/json")
		_, err := writer.Write([]byte(`[{"created_at":"2026-07-20T00:00:00Z","id":7,"role":"admin","username":"alice"}]`))
		require.NoError(t, err)
	}))
	defer server.Close()

	output, err := execute(t,
		"--server", server.URL,
		"--token", "session-token",
		"get", "users",
	)
	require.NoError(t, err)
	require.Contains(t, output, `"username": "alice"`)
}

func TestAuthLoginReadsPasswordFromEnvironment(t *testing.T) {
	t.Setenv("GROUND_CONTROL_PASSWORD", "secret")

	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		require.Equal(t, http.MethodPost, request.Method)
		require.Equal(t, "/login", request.URL.Path)
		var body struct {
			Username string `json:"username"`
			Password string `json:"password"`
		}
		require.NoError(t, json.NewDecoder(request.Body).Decode(&body))
		require.Equal(t, "admin", body.Username)
		require.Equal(t, "secret", body.Password)

		writer.Header().Set("Content-Type", "application/json")
		_, err := writer.Write([]byte(`{"expires_at":"2026-07-21T00:00:00Z","token":"new-token"}`))
		require.NoError(t, err)
	}))
	defer server.Close()

	output, err := execute(t,
		"--server", server.URL,
		"auth", "login",
		"--username", "admin",
	)
	require.NoError(t, err)
	require.Contains(t, output, `"token": "new-token"`)
}

func TestEnvironmentOverridesConfigFile(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		require.Equal(t, "/ping", request.URL.Path)
		writer.Header().Set("Content-Type", "text/plain")
		_, err := writer.Write([]byte("pong-from-env"))
		require.NoError(t, err)
	}))
	defer server.Close()

	configFile := filepath.Join(t.TempDir(), "config.yaml")
	require.NoError(t, os.WriteFile(configFile, []byte("url: http://127.0.0.1:1\n"), 0o600))
	t.Setenv("GROUND_CONTROL_URL", server.URL)

	output, err := execute(t, "--config", configFile, "ping")
	require.NoError(t, err)
	require.Equal(t, "pong-from-env\n", output)
}

func TestCreateConfigAcceptsYAMLManifest(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		require.Equal(t, http.MethodPost, request.Method)
		require.Equal(t, "/api/configs", request.URL.Path)
		require.Equal(t, "Bearer session-token", request.Header.Get("Authorization"))
		var body struct {
			ConfigName string `json:"config_name"`
		}
		require.NoError(t, json.NewDecoder(request.Body).Decode(&body))
		require.Equal(t, "edge", body.ConfigName)
		writer.WriteHeader(http.StatusCreated)
	}))
	defer server.Close()

	manifest := filepath.Join(t.TempDir(), "config.yaml")
	require.NoError(t, os.WriteFile(manifest, []byte("config_name: edge\nconfig:\n  zot_config: {}\n"), 0o600))

	output, err := execute(t,
		"--server", server.URL,
		"--token", "session-token",
		"create", "config",
		"--file", manifest,
	)
	require.NoError(t, err)
	require.Equal(t, "201 Created\n", output)
}

func TestPreRunValidation(t *testing.T) {
	t.Run("token", func(t *testing.T) {
		_, err := execute(t, "get", "users")
		require.ErrorContains(t, err, "authentication token is required")
	})

	t.Run("missing password environment", func(t *testing.T) {
		t.Setenv("GROUND_CONTROL_PASSWORD", "")
		_, err := execute(t, "auth", "login", "--username", "admin")
		require.ErrorContains(t, err, "GROUND_CONTROL_PASSWORD environment variable is required")
	})

	t.Run("whitespace login username", func(t *testing.T) {
		t.Setenv("GROUND_CONTROL_PASSWORD", "secret")
		_, err := execute(t, "auth", "login", "--username", "   ")
		require.ErrorContains(t, err, "--username must not be empty")
	})

	t.Run("password flag removed", func(t *testing.T) {
		_, err := execute(t, "auth", "login", "--username", "admin", "--password", "secret")
		require.ErrorContains(t, err, "unknown flag: --password")
	})

	t.Run("insecure HTTP", func(t *testing.T) {
		_, err := execute(t, "--server", "http://localhost:8080", "--insecure", "ping")
		require.ErrorContains(t, err, "--insecure can only be used with an HTTPS server")
	})
}

func TestExecuteContextCancellationReachesRequest(t *testing.T) {
	t.Parallel()

	requestStarted := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, request *http.Request) {
		close(requestStarted)
		<-request.Context().Done()
	}))
	defer server.Close()

	ctx, cancel := context.WithCancel(context.Background())
	command := cli.RootCmd()
	command.SetArgs([]string{"--server", server.URL, "ping"})
	command.SetOut(&bytes.Buffer{})

	done := make(chan error, 1)
	go func() {
		done <- command.ExecuteContext(ctx)
	}()
	<-requestStarted
	cancel()

	err := <-done
	require.Error(t, err)
	require.True(t, strings.Contains(err.Error(), "context canceled"), err.Error())
}

func execute(t *testing.T, args ...string) (string, error) {
	t.Helper()

	output := &bytes.Buffer{}
	command := cli.RootCmd()
	command.SetOut(output)
	command.SetErr(output)
	command.SetArgs(args)
	err := command.ExecuteContext(context.Background())
	return output.String(), err
}
