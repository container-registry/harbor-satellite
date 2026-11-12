package auth

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/container-registry/harbor-satellite/pkg/groundcontrol"
	"github.com/stretchr/testify/require"
)

func TestLoginOIDCFlag_DefaultOff(t *testing.T) {
	cmd := NewLoginCommand(nil)
	flag := cmd.Flags().Lookup("oidc")
	require.NotNil(t, flag)
	require.Equal(t, "false", flag.DefValue)

	require.NoError(t, cmd.Flags().Set("oidc", "true"))
	require.NoError(t, cmd.PreRunE(cmd, nil), "the device flow needs no --username")

	plain := NewLoginCommand(nil)
	plain.SetArgs([]string{})
	plain.SilenceUsage, plain.SilenceErrors = true, true
	require.ErrorContains(t, plain.Execute(), "--username", "a password login still needs one")
}

// deviceGC answers the device authorization, then pending, slow_down and
// a session.
func deviceGC(t *testing.T) (*groundcontrol.ClientWithResponses, *[]string) {
	t.Helper()
	var polls []string
	answers := []struct {
		status int
		body   any
	}{
		{http.StatusBadRequest, groundcontrol.AppError{Code: 400, Message: "authorization_pending"}},
		{http.StatusBadRequest, groundcontrol.AppError{Code: 400, Message: "slow_down"}},
		{http.StatusOK, groundcontrol.LoginResponse{Token: "session-1", ExpiresAt: time.Date(2026, 11, 12, 0, 0, 0, 0, time.UTC)}},
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/oidc/device":
			require.NoError(t, json.NewEncoder(w).Encode(groundcontrol.DeviceAuthorizationResponse{DeviceCode: "dev-1", UserCode: "ABCD-EFGH", VerificationURI: "https://idp.example/device", ExpiresIn: 600, Interval: 2}))
		case "/oidc/device/token":
			var req groundcontrol.DeviceTokenRequest
			require.NoError(t, json.NewDecoder(r.Body).Decode(&req))
			polls = append(polls, req.DeviceCode)
			a := answers[min(len(polls), len(answers))-1]
			w.WriteHeader(a.status)
			require.NoError(t, json.NewEncoder(w).Encode(a.body))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	client, err := groundcontrol.NewClientWithResponses(srv.URL)
	require.NoError(t, err)
	return client, &polls
}

func TestDeviceLogin_PollsUntilSessionAndSlowsDown(t *testing.T) {
	client, polls := deviceGC(t)
	var out bytes.Buffer
	var waits []time.Duration

	session, err := deviceLogin(context.Background(), client, &out, func(_ context.Context, d time.Duration) error {
		waits = append(waits, d)
		return nil
	})

	require.NoError(t, err)
	require.Equal(t, "session-1", session.Token)
	require.Equal(t, []string{"dev-1", "dev-1", "dev-1"}, *polls)
	require.Equal(t, []time.Duration{2 * time.Second, 2 * time.Second, 7 * time.Second}, waits, "slow_down adds 5 seconds")
	require.True(t, strings.Contains(out.String(), "ABCD-EFGH"), out.String())
}
