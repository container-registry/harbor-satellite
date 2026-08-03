package state

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/container-registry/harbor-satellite/pkg/config"
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

		var body map[string]string
		require.NoError(t, json.NewDecoder(r.Body).Decode(&body))
		require.Equal(t, token, body["token"])

		w.Header().Set("Content-Type", "application/json")
		_, err := w.Write([]byte(`{}`))
		require.NoError(t, err)
	}))
	defer server.Close()

	_, err := registerSatellite(server.URL, ZeroTouchRegistrationRoute, token, config.TLSConfig{}, false, context.Background())
	require.NoError(t, err)
}

func TestSanitizeAuditReason_TokenAppearsMultipleTimes(t *testing.T) {
	token := "tk-12345"
	err := errors.New("first url " + token + " and again " + token + " here")

	got := sanitizeAuditReason(err, token)
	require.Equal(t, 2, strings.Count(got, "[REDACTED]"))
	require.NotContains(t, got, token)
}
