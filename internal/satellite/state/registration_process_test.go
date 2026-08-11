package state

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/container-registry/harbor-satellite/pkg/config"
	"github.com/container-registry/harbor-satellite/pkg/groundcontrol"
	"github.com/stretchr/testify/require"
)

func TestSanitizeAuditReason_RedactsToken(t *testing.T) {
	token := "supersecret-abcdef0123456789"
	err := errors.New("registration request failed for token " + token + ": connection refused")

	got := sanitizeAuditReason(err, token)

	require.NotContains(t, got, token, "token must not appear in sanitized reason")
	require.Contains(t, got, "[REDACTED]")
	require.Contains(t, got, "connection refused", "diagnostic context should be preserved")
}

func TestSanitizeAuditReason_NilError(t *testing.T) {
	require.Empty(t, sanitizeAuditReason(nil, "any-token"))
}

func TestSanitizeAuditReason_EmptyToken(t *testing.T) {
	err := errors.New("registration failed")
	require.Equal(t, "registration failed", sanitizeAuditReason(err, ""))
}

func TestRegisterSatellitePostsTokenInJSONBody(t *testing.T) {
	token := "supersecret-abcdef0123456789"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, http.MethodPost, r.Method)
		require.Equal(t, "/satellites/ztr", r.URL.Path)
		require.Equal(t, "application/json", r.Header.Get("Content-Type"))

		var body groundcontrol.ZTRRequest
		require.NoError(t, json.NewDecoder(r.Body).Decode(&body))
		require.Equal(t, token, body.Token)

		w.Header().Set("Content-Type", "application/json")
		_, err := w.Write([]byte(`{"auth":{"url":"registry.test","username":"robot","password":"secret"},"state":"registry.test/state:latest"}`))
		require.NoError(t, err)
	}))
	defer server.Close()

	stateConfig, err := registerSatellite(server.URL, token, config.TLSConfig{}, true, context.Background())
	require.NoError(t, err)
	require.Equal(t, config.URL("registry.test"), stateConfig.RegistryCredentials.URL)
	require.Equal(t, "robot", stateConfig.RegistryCredentials.Username)
	require.Equal(t, "secret", stateConfig.RegistryCredentials.Password)
	require.Equal(t, "registry.test/state:latest", stateConfig.StateURL)
}

func TestRegisterSatelliteReturnsTypedError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnauthorized)
		_, err := w.Write([]byte(`{"code":40101,"message":"registration token expired"}`))
		require.NoError(t, err)
	}))
	defer server.Close()

	_, err := registerSatellite(server.URL, "expired-token", config.TLSConfig{}, true, context.Background())

	require.ErrorContains(t, err, "401 Unauthorized")
	require.ErrorContains(t, err, "code=40101")
	require.ErrorContains(t, err, `message="registration token expired"`)
}

func TestRegisterSatellitePreservesUnknownResponseContext(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusTeapot)
		_, err := w.Write([]byte("unexpected proxy response"))
		require.NoError(t, err)
	}))
	defer server.Close()

	_, err := registerSatellite(server.URL, "token", config.TLSConfig{}, true, context.Background())

	require.ErrorContains(t, err, "418 I'm a teapot")
	require.ErrorContains(t, err, "unexpected proxy response")
}

func TestZtrProcessDoesNotPersistInvalidRegistrationData(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, err := w.Write([]byte(`{"auth":{"url":"new-registry","username":"new-user"},"state":"new-state"}`))
		require.NoError(t, err)
	}))
	defer server.Close()

	existingState := config.StateConfig{
		RegistryCredentials: config.RegistryCredentials{
			URL:      "existing-registry",
			Username: "existing-user",
			Password: "existing-password",
		},
		StateURL: "existing-state",
	}
	cfg := &config.Config{
		StateConfig: existingState,
		AppConfig: config.AppConfig{
			GroundControlURL: config.URL(server.URL),
			UseUnsecure:      true,
		},
		ZotConfigRaw: json.RawMessage(`{}`),
	}
	dir := t.TempDir()
	cm, err := config.NewConfigManager(
		filepath.Join(dir, "config.json"),
		filepath.Join(dir, "prev.json"),
		"token", server.URL, false, cfg,
	)
	require.NoError(t, err)

	err = NewZtrProcess(cm).Execute(testContext())

	require.ErrorContains(t, err, "invalid state auth config")
	require.Equal(t, existingState, cm.GetStateConfig())
}

func TestSanitizeAuditReason_TokenAppearsMultipleTimes(t *testing.T) {
	token := "tk-12345"
	err := errors.New("first url " + token + " and again " + token + " here")

	got := sanitizeAuditReason(err, token)
	require.Equal(t, 2, strings.Count(got, "[REDACTED]"))
	require.NotContains(t, got, token)
}
