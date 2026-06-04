package server

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestRedactConfigForAudit_RedactsSecretsKeepsRest(t *testing.T) {
	raw := []byte(`{
		"state_config": {"auth": {"username": "robot$sat", "password": "s3cret", "url": "http://h"}},
		"app_config": {"log_level": "info", "tls": {"key_file": "/etc/k.pem", "secret_token": "abc"}},
		"list": [{"password": "p"}, {"keep": "ok"}]
	}`)

	// Round-trip the redacted value through JSON to inspect it.
	b, err := json.Marshal(redactConfigForAudit(raw))
	require.NoError(t, err)
	var m map[string]any
	require.NoError(t, json.Unmarshal(b, &m))

	auth := m["state_config"].(map[string]any)["auth"].(map[string]any)
	require.Equal(t, auditRedacted, auth["password"], "password must be redacted")
	require.Equal(t, "robot$sat", auth["username"], "non-secret username preserved")
	require.Equal(t, "http://h", auth["url"], "non-secret url preserved")

	tls := m["app_config"].(map[string]any)["tls"].(map[string]any)
	require.Equal(t, "/etc/k.pem", tls["key_file"], "a file path is not a secret value, preserved")
	require.Equal(t, auditRedacted, tls["secret_token"], "secret_token must be redacted")

	list := m["list"].([]any)
	require.Equal(t, auditRedacted, list[0].(map[string]any)["password"], "password nested in an array must be redacted")
	require.Equal(t, "ok", list[1].(map[string]any)["keep"], "non-secret in array preserved")

	// Belt-and-suspenders: the secret value must not survive anywhere in output.
	require.NotContains(t, string(b), "s3cret")
}

func TestRedactConfigForAudit_EdgeCases(t *testing.T) {
	require.Nil(t, redactConfigForAudit(nil), "empty input -> nil (field omitted)")
	require.Nil(t, redactConfigForAudit([]byte{}), "empty input -> nil")
	require.Equal(t, "[unparseable config]", redactConfigForAudit([]byte("}{ not json")),
		"undecodable input must NOT echo raw bytes")
}

// asJSON round-trips a value through JSON so map assertions are free of Go
// numeric-type noise.
func asJSON(t *testing.T, v any) map[string]any {
	t.Helper()
	b, err := json.Marshal(v)
	require.NoError(t, err)
	var m map[string]any
	require.NoError(t, json.Unmarshal(b, &m))
	return m
}

func TestDiffConfigForAudit_SecretRotationIsVisibleButRedacted(t *testing.T) {
	// The exact case that exposed the gap: ONLY the password changed.
	old := []byte(`{"state_config":{"auth":{"username":"u","password":"OLDSECRET","url":"http://h"}}}`)
	new := []byte(`{"state_config":{"auth":{"username":"u","password":"NEWSECRET","url":"http://h"}}}`)

	diff := diffConfigForAudit(old, new)
	m := asJSON(t, diff)

	// The changed password is reported by path, so the rotation IS visible...
	entry, ok := m["state_config.auth.password"].(map[string]any)
	require.True(t, ok, "the rotated password must appear as a changed path")
	require.Equal(t, auditRedacted, entry["from"], "...but the old value is redacted")
	require.Equal(t, auditRedacted, entry["to"], "...and the new value is redacted")

	// Unchanged fields are not reported.
	require.NotContains(t, m, "state_config.auth.username")
	require.NotContains(t, m, "state_config.auth.url")

	// Neither raw secret leaks.
	b, _ := json.Marshal(diff)
	require.NotContains(t, string(b), "OLDSECRET")
	require.NotContains(t, string(b), "NEWSECRET")
}

func TestDiffConfigForAudit_NonSecretShowsRealValues(t *testing.T) {
	old := []byte(`{"app_config":{"log_level":"info"}}`)
	new := []byte(`{"app_config":{"log_level":"debug"}}`)
	m := asJSON(t, diffConfigForAudit(old, new))
	entry := m["app_config.log_level"].(map[string]any)
	require.Equal(t, "info", entry["from"])
	require.Equal(t, "debug", entry["to"])
}

func TestDiffConfigForAudit_AddRemoveAndNoChange(t *testing.T) {
	require.Nil(t, diffConfigForAudit([]byte(`{"a":1}`), []byte(`{"a":1}`)), "no change -> nil")

	added := asJSON(t, diffConfigForAudit([]byte(`{"a":1}`), []byte(`{"a":1,"b":2}`)))
	require.Contains(t, added, "b")
	require.NotContains(t, added["b"].(map[string]any), "from", "added key has only 'to'")

	removed := asJSON(t, diffConfigForAudit([]byte(`{"a":1,"b":2}`), []byte(`{"a":1}`)))
	require.Contains(t, removed, "b")
	require.NotContains(t, removed["b"].(map[string]any), "to", "removed key has only 'from'")

	// A key going from null to absent (or absent to null) is not a real change.
	require.Nil(t, diffConfigForAudit([]byte(`{"a":1,"z":null}`), []byte(`{"a":1}`)), "null -> absent is not a change")
	require.Nil(t, diffConfigForAudit([]byte(`{"a":1}`), []byte(`{"a":1,"z":null}`)), "absent -> null is not a change")
}
