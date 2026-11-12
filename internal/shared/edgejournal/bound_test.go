package edgejournal

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestBoundPayload_LeavesShortPayloadsAlone(t *testing.T) {
	in := json.RawMessage(`{"b":1, "a":"x"}`)
	require.Equal(t, in, BoundPayload(in))
	notJSON := json.RawMessage(strings.Repeat("{", 2000))
	require.Equal(t, notJSON, BoundPayload(notJSON))
	require.Equal(t, "short", Truncate("short"))
}
