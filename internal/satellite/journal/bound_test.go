package journal

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/container-registry/harbor-satellite/internal/shared/edgejournal"
	"github.com/stretchr/testify/require"
)

// An error text is unbounded (a registry may answer with an HTML page);
// the journal stores at most edgejournal.MaxStringBytes of every string.
func TestJournal_BoundsPayloadStringsAtWrite(t *testing.T) {
	j, err := Open(Options{Dir: t.TempDir()})
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, j.Close()) })

	long := strings.Repeat("ä", 5000)
	payload, err := json.Marshal(edgejournal.Replication{SchemaVersion: edgejournal.SchemaVersion, Repository: "library/app", Bytes: 1<<62 + 1, Error: long})
	require.NoError(t, err)
	_, err = j.Append(context.Background(), Record{Type: edgejournal.RecordReplication, RecordedAt: time.Now(), Payload: payload})
	require.NoError(t, err)
	require.True(t, j.TryAppend(Record{Type: edgejournal.RecordPullThrough, RecordedAt: time.Now(), Payload: json.RawMessage(`{"schema_version":"1","reference":"` + strings.Repeat("r", 3000) + `"}`)}))
	require.Eventually(t, func() bool { return j.LastSeq() == 2 }, 2*time.Second, 5*time.Millisecond)

	records, err := j.ReadFrom(0, 10)
	require.NoError(t, err)
	require.Len(t, records, 2)
	var got edgejournal.Replication
	require.NoError(t, json.Unmarshal(records[0].Payload, &got))
	require.LessOrEqual(t, len(got.Error), edgejournal.MaxStringBytes)
	require.True(t, utf8.ValidString(got.Error), "cut at a rune boundary")
	require.True(t, strings.HasPrefix(long, strings.TrimSuffix(got.Error, "...[truncated]")))
	require.Equal(t, int64(1<<62+1), got.Bytes, "numbers keep their exact value")
	require.Equal(t, "library/app", got.Repository)
	var pull edgejournal.PullThrough
	require.NoError(t, json.Unmarshal(records[1].Payload, &pull))
	require.Len(t, pull.Reference, edgejournal.MaxStringBytes)
}
