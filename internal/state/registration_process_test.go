package state

import (
	"errors"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestSanitizeAuditReason_RedactsToken(t *testing.T) {
	token := "supersecret-abcdef0123456789"
	err := errors.New("Get \"http://gc:8080/satellites/ztr/" + token + "\": dial tcp: connection refused")

	got := sanitizeAuditReason(err, token)

	require.NotContains(t, got, token, "token must not appear in sanitized reason")
	require.Contains(t, got, "[REDACTED]")
	require.Contains(t, got, "connection refused", "diagnostic context should be preserved")
}

func TestSanitizeAuditReason_NilError(t *testing.T) {
	require.Equal(t, "", sanitizeAuditReason(nil, "any-token"))
}

func TestSanitizeAuditReason_EmptyToken(t *testing.T) {
	err := errors.New("registration failed")
	require.Equal(t, "registration failed", sanitizeAuditReason(err, ""))
}

func TestSanitizeAuditReason_TokenAppearsMultipleTimes(t *testing.T) {
	token := "tk-12345"
	err := errors.New("first url " + token + " and again " + token + " here")

	got := sanitizeAuditReason(err, token)
	require.Equal(t, 2, strings.Count(got, "[REDACTED]"))
	require.NotContains(t, got, token)
}
