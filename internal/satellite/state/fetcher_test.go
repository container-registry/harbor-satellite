package state

import (
	"bytes"
	"encoding/json"
	"testing"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/require"
)

func TestFromJSONLogsStructuredUnmarshalError(t *testing.T) {
	var buf bytes.Buffer
	log := zerolog.New(&buf)

	_, err := FromJSON([]byte("{invalid"), NewState(), &log)
	require.Error(t, err)
	require.Contains(t, err.Error(), "unmarshal state")

	var syntaxErr *json.SyntaxError
	require.ErrorAs(t, err, &syntaxErr)

	got := buf.String()
	require.NotEmpty(t, got)
	require.True(t, json.Valid([]byte(got)))

	var entry map[string]any
	require.NoError(t, json.Unmarshal([]byte(got), &entry))
	require.Equal(t, "error", entry["level"])
	require.Equal(t, "Error in unmarshalling", entry["message"])

	errorField, ok := entry["error"].(string)
	require.True(t, ok)
	require.Contains(t, errorField, "invalid character")
}
