package auth_test

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/container-registry/harbor-satellite/internal/groundcontrol/cli"
	"github.com/stretchr/testify/require"
)

// runCLI executes the groundcontrol CLI against a credentials file the caller
// owns, so a login and a later whoami share one stored session.
func runCLI(t *testing.T, credentialsFile string, args ...string) (string, error) {
	t.Helper()
	t.Setenv("GROUND_CONTROL_CREDENTIALS_FILE", credentialsFile)
	out := &bytes.Buffer{}
	cmd := cli.RootCmd()
	cmd.SetOut(out)
	cmd.SetErr(out)
	cmd.SetArgs(args)
	err := cmd.ExecuteContext(context.Background())
	return out.String(), err
}

// loginServer answers every request with a session that expires at expiry.
func loginServer(t *testing.T, expiry time.Time) *httptest.Server {
	t.Helper()
	body := fmt.Sprintf(`{"token":"session-token","expires_at":%q}`, expiry.Format(time.RFC3339))
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if _, err := io.WriteString(w, body); err != nil {
			t.Errorf("write response: %v", err)
		}
	}))
	t.Cleanup(server.Close)
	return server
}

func login(t *testing.T, credentialsFile, serverURL string) {
	t.Helper()
	t.Setenv("GROUND_CONTROL_PASSWORD", "Passw0rd")
	_, err := runCLI(t, credentialsFile, "--server", serverURL, "auth", "login", "--username", "operator")
	require.NoError(t, err)
}

func TestWhoamiShowsTheStoredSession(t *testing.T) {
	credentialsFile := filepath.Join(t.TempDir(), "credentials.json")
	expiry := time.Now().Add(time.Hour).UTC().Round(time.Second)
	server := loginServer(t, expiry)

	login(t, credentialsFile, server.URL)
	out, err := runCLI(t, credentialsFile, "--server", server.URL, "auth", "whoami")

	require.NoError(t, err)
	require.Contains(t, out, server.URL)
	require.Contains(t, out, "operator")
	require.Contains(t, out, expiry.Format(time.RFC3339))
	require.NotContains(t, out, "expired")
}

func TestWhoamiMarksAnExpiredSession(t *testing.T) {
	credentialsFile := filepath.Join(t.TempDir(), "credentials.json")
	expiry := time.Now().Add(-time.Minute).UTC().Round(time.Second)
	server := loginServer(t, expiry)

	login(t, credentialsFile, server.URL)
	// whoami must not purge the expired session, so a second call still reports it.
	for range 2 {
		out, err := runCLI(t, credentialsFile, "--server", server.URL, "auth", "whoami")

		require.NoError(t, err)
		require.Contains(t, out, "(expired)")
	}
}

func TestWhoamiExitsNonZeroWithoutASession(t *testing.T) {
	credentialsFile := filepath.Join(t.TempDir(), "credentials.json")
	out, err := runCLI(t, credentialsFile, "--server", "https://gc.example", "auth", "whoami")
	require.ErrorContains(t, err, "no session stored")
	require.Contains(t, out, "no session stored")
}
