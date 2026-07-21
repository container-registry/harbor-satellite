package cli_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/container-registry/harbor-satellite/internal/groundcontrol/cli"
	"github.com/stretchr/testify/require"
)

var credentialsFile string

func TestMain(testingMain *testing.M) {
	testConfigDir, err := os.MkdirTemp("", "groundcontrol-cli-test-")
	if err != nil {
		panic(err)
	}
	credentialsFile = filepath.Join(testConfigDir, "credentials.json")
	if err := os.Setenv("GROUND_CONTROL_CREDENTIALS_FILE", credentialsFile); err != nil {
		panic(err)
	}

	exitCode := testingMain.Run()
	_ = os.Unsetenv("GROUND_CONTROL_CREDENTIALS_FILE")
	_ = os.RemoveAll(testConfigDir)
	os.Exit(exitCode)
}

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

func TestAuthLoginStoresTokenAndHonorsPrecedence(t *testing.T) {
	t.Setenv("GROUND_CONTROL_PASSWORD", "secret")
	var authHeaders []string
	var logoutHeaders []string
	var authHeadersMutex sync.Mutex

	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/login":
			require.Equal(t, http.MethodPost, request.Method)
			var body struct {
				Username string `json:"username"`
				Password string `json:"password"`
			}
			require.NoError(t, json.NewDecoder(request.Body).Decode(&body))
			require.Equal(t, "admin", body.Username)
			require.Equal(t, "secret", body.Password)

			writer.Header().Set("Content-Type", "application/json")
			_, err := fmt.Fprintf(
				writer,
				`{"expires_at":%q,"token":"stored-token"}`,
				time.Now().Add(time.Hour).UTC().Format(time.RFC3339),
			)
			require.NoError(t, err)
		case "/api/users":
			authHeadersMutex.Lock()
			authHeaders = append(authHeaders, request.Header.Get("Authorization"))
			authHeadersMutex.Unlock()
			writer.Header().Set("Content-Type", "application/json")
			_, err := writer.Write([]byte("[]"))
			require.NoError(t, err)
		case "/api/logout":
			authHeadersMutex.Lock()
			logoutHeaders = append(logoutHeaders, request.Header.Get("Authorization"))
			authHeadersMutex.Unlock()
			writer.WriteHeader(http.StatusOK)
		default:
			http.NotFound(writer, request)
		}
	}))
	defer server.Close()

	output, err := execute(t,
		"--server", server.URL,
		"auth", "login",
		"--username", "admin",
	)
	require.NoError(t, err)
	require.Empty(t, output)
	require.FileExists(t, credentialsFile)
	t.Cleanup(func() {
		_, _ = execute(t, "--server", server.URL, "auth", "logout")
	})

	_, err = execute(t, "--server", server.URL, "get", "users")
	require.NoError(t, err)
	_, err = execute(t, "--server", server.URL, "--token", "one-off-token", "auth", "logout")
	require.NoError(t, err)
	_, err = execute(t, "--server", server.URL, "get", "users")
	require.NoError(t, err)

	configFile := filepath.Join(t.TempDir(), "config.yaml")
	require.NoError(t, os.WriteFile(configFile, []byte("token: config-token\n"), 0o600))
	_, err = execute(t, "--server", server.URL, "--config", configFile, "get", "users")
	require.NoError(t, err)
	_, err = execute(t,
		"--server", server.URL,
		"--config", configFile,
		"--token", "flag-token",
		"get", "users",
	)
	require.NoError(t, err)

	authHeadersMutex.Lock()
	require.Equal(t, []string{
		"Bearer stored-token",
		"Bearer stored-token",
		"Bearer config-token",
		"Bearer flag-token",
	}, authHeaders)
	authHeadersMutex.Unlock()

	_, err = execute(t, "--server", server.URL, "auth", "logout")
	require.NoError(t, err)
	authHeadersMutex.Lock()
	require.Equal(t, []string{"Bearer one-off-token", "Bearer stored-token"}, logoutHeaders)
	authHeadersMutex.Unlock()
	_, err = execute(t, "--server", server.URL, "get", "users")
	require.ErrorContains(t, err, "authentication token is required")
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

func TestCreateUserUsesPasswordEnvironment(t *testing.T) {
	t.Setenv("GROUND_CONTROL_USER_PASSWORD", " user password ")

	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		require.Equal(t, http.MethodPost, request.Method)
		require.Equal(t, "/api/users", request.URL.Path)
		require.Equal(t, "Bearer session-token", request.Header.Get("Authorization"))
		var body struct {
			Username string `json:"username"`
			Password string `json:"password"`
		}
		require.NoError(t, json.NewDecoder(request.Body).Decode(&body))
		require.Equal(t, "alice", body.Username)
		require.Equal(t, " user password ", body.Password)
		writer.WriteHeader(http.StatusCreated)
	}))
	defer server.Close()

	output, err := execute(t,
		"--server", server.URL,
		"--token", "session-token",
		"create", "user",
		"--username", "alice",
	)
	require.NoError(t, err)
	require.Equal(t, "201 Created\n", output)
}

func TestUpdateConfigPreservesNull(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		require.Equal(t, http.MethodPatch, request.Method)
		require.Equal(t, "/api/configs/edge", request.URL.Path)
		require.Equal(t, "Bearer session-token", request.Header.Get("Authorization"))
		var body map[string]json.RawMessage
		require.NoError(t, json.NewDecoder(request.Body).Decode(&body))
		require.JSONEq(t, "null", string(body["app_config"]))
		writer.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	manifest := filepath.Join(t.TempDir(), "patch.yaml")
	require.NoError(t, os.WriteFile(manifest, []byte("app_config: null\n"), 0o600))
	output, err := execute(t,
		"--server", server.URL,
		"--token", "session-token",
		"update", "config",
		"--name", "edge",
		"--file", manifest,
	)
	require.NoError(t, err)
	require.Equal(t, "200 OK\n", output)
}

func TestUpdateUserPasswordPreservesWhitespace(t *testing.T) {
	t.Setenv("GROUND_CONTROL_NEW_PASSWORD", " new password ")

	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		require.Equal(t, http.MethodPatch, request.Method)
		require.Equal(t, "/api/users/alice/password", request.URL.Path)
		var body struct {
			NewPassword string `json:"new_password"`
		}
		require.NoError(t, json.NewDecoder(request.Body).Decode(&body))
		require.Equal(t, " new password ", body.NewPassword)
		writer.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	_, err := execute(t,
		"--server", server.URL,
		"--token", "session-token",
		"update", "user-password",
		"--username", "alice",
	)
	require.NoError(t, err)
}

func TestUpdateOwnPasswordRemovesStoredToken(t *testing.T) {
	require.NoError(t, os.RemoveAll(credentialsFile))
	t.Setenv("GROUND_CONTROL_PASSWORD", "login-password")
	t.Setenv("GROUND_CONTROL_CURRENT_PASSWORD", "current-password")
	t.Setenv("GROUND_CONTROL_NEW_PASSWORD", "new-password")

	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/login":
			writer.Header().Set("Content-Type", "application/json")
			_, err := fmt.Fprintf(
				writer,
				`{"expires_at":%q,"token":"stored-token"}`,
				time.Now().Add(time.Hour).UTC().Format(time.RFC3339),
			)
			require.NoError(t, err)
		case "/api/users/password":
			require.Equal(t, "Bearer stored-token", request.Header.Get("Authorization"))
			writer.WriteHeader(http.StatusOK)
		default:
			http.NotFound(writer, request)
		}
	}))
	defer server.Close()

	_, err := execute(t, "--server", server.URL, "auth", "login", "--username", "admin")
	require.NoError(t, err)
	_, err = execute(t, "--server", server.URL, "update", "own-password")
	require.NoError(t, err)
	_, err = execute(t, "--server", server.URL, "get", "users")
	require.ErrorContains(t, err, "authentication token is required")
}

func TestUnauthorizedStoredTokenIsRemoved(t *testing.T) {
	require.NoError(t, os.RemoveAll(credentialsFile))
	t.Setenv("GROUND_CONTROL_PASSWORD", "login-password")

	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/login":
			writer.Header().Set("Content-Type", "application/json")
			_, err := fmt.Fprintf(
				writer,
				`{"expires_at":%q,"token":"stored-token"}`,
				time.Now().Add(time.Hour).UTC().Format(time.RFC3339),
			)
			require.NoError(t, err)
		case "/api/users":
			require.Equal(t, "Bearer stored-token", request.Header.Get("Authorization"))
			writer.WriteHeader(http.StatusUnauthorized)
		default:
			http.NotFound(writer, request)
		}
	}))
	defer server.Close()

	_, err := execute(t, "--server", server.URL, "auth", "login", "--username", "admin")
	require.NoError(t, err)
	_, err = execute(t, "--server", server.URL, "get", "users")
	require.ErrorContains(t, err, "401 Unauthorized")
	_, err = execute(t, "--server", server.URL, "get", "users")
	require.ErrorContains(t, err, "authentication token is required")
}

func TestSpireAgentFilterIsTrimmed(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		require.Equal(t, "/api/spire/agents", request.URL.Path)
		require.Equal(t, "x509pop", request.URL.Query().Get("attestation_type"))
		writer.Header().Set("Content-Type", "application/json")
		_, err := writer.Write([]byte(`{"agents":[]}`))
		require.NoError(t, err)
	}))
	defer server.Close()

	_, err := execute(t,
		"--server", server.URL,
		"--token", "session-token",
		"get", "spire-agents",
		"--attestation-type", " x509pop ",
	)
	require.NoError(t, err)
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
	output := &bytes.Buffer{}
	command.SetOut(output)
	command.SetErr(output)

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
