package logger

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestDiffConfigPreservesLargeIntegers(t *testing.T) {
	t.Parallel()

	diff := DiffConfig(
		[]byte(`{"sequence":9007199254740992}`),
		[]byte(`{"sequence":9007199254740993}`),
	)

	change, ok := diff["sequence"].(map[string]any)
	require.True(t, ok)
	require.Equal(t, json.Number("9007199254740992"), change["from"])
	require.Equal(t, json.Number("9007199254740993"), change["to"])
}

func TestRedactConfigNormalizesSensitiveKeyNames(t *testing.T) {
	t.Parallel()

	redacted, ok := RedactConfig([]byte(`{
		"privateKey":"one",
		"private-key":"two",
		"apiKey":"three",
		"api-key":"four",
		"accessKey":"five",
		"access-key":"six",
		"clientCredential":"seven",
		"safe_key":"visible"
	}`)).(map[string]any)
	require.True(t, ok)

	for _, key := range []string{
		"privateKey", "private-key", "apiKey", "api-key",
		"accessKey", "access-key", "clientCredential",
	} {
		require.Equal(t, RedactedValue, redacted[key], key)
	}
	require.Equal(t, "visible", redacted["safe_key"])
}

func TestRedactConfigRejectsTrailingJSON(t *testing.T) {
	t.Parallel()

	require.Equal(t, "[unparseable config]", RedactConfig([]byte(`{} {}`)))
}
