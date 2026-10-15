package server

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/container-registry/harbor-satellite/internal/shared/edgejournal"
	"github.com/container-registry/harbor-satellite/internal/shared/testutil/pending"
	"github.com/stretchr/testify/require"
)

const journalEpoch int64 = 1760529600000

func replicationRecords(t *testing.T, seqs ...int64) []JournalRecord {
	t.Helper()
	payload, err := json.Marshal(edgejournal.Replication{
		SchemaVersion: edgejournal.SchemaVersion, Action: edgejournal.ActionReplicated, Group: "edge-group",
		Repository: "library/alpine", Tag: "latest", Digest: "sha256:aaa",
	})
	require.NoError(t, err)
	out := make([]JournalRecord, 0, len(seqs))
	for _, seq := range seqs {
		out = append(out, JournalRecord{Seq: seq, Type: string(edgejournal.RecordReplication), RecordedAt: time.Date(2026, 10, 15, 12, 0, int(seq), 0, time.UTC), Payload: payload})
	}
	return out
}

// edgeEvents counts the log-only edge events in the outbox; the tests
// onboard one satellite.
func (h *gcHarness) edgeEvents() int {
	h.t.Helper()
	var n int
	require.NoError(h.t, h.db.QueryRowContext(context.Background(),
		"SELECT count(*) FROM event_outbox WHERE aggregate_type = 'edge' AND done_at IS NOT NULL").Scan(&n))
	return n
}

func TestSync_IngestsJournalOnceAndAcks(t *testing.T) {
	pending.Plan(t, "6.5", "the sync handler ignores the uploaded journal")
	h := newGCHarness(t)
	h.createConfig("edge-config")
	h.syncGroup("edge-group", "library/alpine")
	creds := h.onboard("edge-01", "edge-config", "edge-group")
	auth := basicAuth(creds.Username, creds.Password)
	beat := heartbeatRequest("edge-01")
	beat.Journal = JournalUpload{Epoch: journalEpoch, AfterSeq: 0, Records: replicationRecords(t, 1, 2, 3)}

	for attempt := range 2 {
		var resp SatelliteSyncResponse
		h.mustDo(http.MethodPost, "/satellites/sync", beat, auth, http.StatusOK).decode(t, &resp)
		require.NotNil(t, resp.Journal, "upload %d is answered", attempt+1)
		require.Equal(t, JournalAck{Epoch: journalEpoch, AckedSeq: 3}, *resp.Journal, "upload %d", attempt+1)
	}
	require.Equal(t, 3, h.edgeEvents(), "a retried upload emits no event twice")

	delayed := beat
	delayed.Journal.Epoch = journalEpoch - 1
	var resp SatelliteSyncResponse
	h.mustDo(http.MethodPost, "/satellites/sync", delayed, auth, http.StatusOK).decode(t, &resp)
	require.NotNil(t, resp.Journal)
	require.Equal(t, JournalAck{Epoch: journalEpoch}, *resp.Journal, "an older epoch learns Ground Control's")
	require.Equal(t, 3, h.edgeEvents())

	floor := beat
	floor.Journal = JournalUpload{Epoch: journalEpoch, AfterSeq: 10, Records: replicationRecords(t, 11)}
	h.mustDo(http.MethodPost, "/satellites/sync", floor, auth, http.StatusOK).decode(t, &resp)
	require.Equal(t, JournalAck{Epoch: journalEpoch, AckedSeq: 11}, *resp.Journal)
	require.Equal(t, 5, h.edgeEvents(), "a gap event for 4 to 10, then seq 11")
}

// A125: a malformed journal is answered in the response and never fails
// the heartbeat.
func TestSync_MalformedJournalKeepsTheHeartbeat(t *testing.T) {
	pending.Plan(t, "6.5", "the sync handler ignores the uploaded journal")
	h := newGCHarness(t)
	h.createConfig("edge-config")
	h.syncGroup("edge-group", "library/alpine")
	creds := h.onboard("edge-01", "edge-config", "edge-group")
	seqs := make([]int64, 1001)
	for i := range seqs {
		seqs[i] = int64(i + 1)
	}
	beat := heartbeatRequest("edge-01")
	beat.Journal = JournalUpload{Epoch: journalEpoch, AfterSeq: 0, Records: replicationRecords(t, seqs...)}

	var resp SatelliteSyncResponse
	h.mustDo(http.MethodPost, "/satellites/sync", beat, basicAuth(creds.Username, creds.Password), http.StatusOK).decode(t, &resp)

	require.NotNil(t, resp.Journal)
	require.NotEmpty(t, resp.Journal.Error)
	require.Equal(t, JournalAck{Epoch: journalEpoch, AckedSeq: 0, Error: resp.Journal.Error}, *resp.Journal, "no ack advance")
	require.Zero(t, h.edgeEvents())
	var lastSeen *time.Time
	require.NoError(t, h.db.QueryRowContext(context.Background(), "SELECT last_seen FROM satellites WHERE name = $1", "edge-01").Scan(&lastSeen))
	require.NotNil(t, lastSeen, "the heartbeat itself was recorded")
}

// A131: a heartbeat may carry up to 1000 records, so the endpoint bounds
// what it reads.
func TestSync_RejectsAnOversizedBody(t *testing.T) {
	pending.Plan(t, "6.5", "POST /satellites/sync reads bodies of any size")
	t.Setenv("SYNC_MAX_BODY_BYTES", "4096")
	h := newGCHarness(t)
	h.createConfig("edge-config")
	h.syncGroup("edge-group", "library/alpine")
	creds := h.onboard("edge-01", "edge-config", "edge-group")
	auth := basicAuth(creds.Username, creds.Password)
	h.mustDo(http.MethodPost, "/satellites/sync", heartbeatRequest("edge-01"), auth, http.StatusOK)

	big := heartbeatRequest("edge-01")
	big.Activity = strings.Repeat("x", 8192)
	h.mustDo(http.MethodPost, "/satellites/sync", big, auth, http.StatusRequestEntityTooLarge)
}
