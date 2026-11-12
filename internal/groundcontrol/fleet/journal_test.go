package fleet_test

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/container-registry/harbor-satellite/internal/groundcontrol/fleet"
	"github.com/container-registry/harbor-satellite/internal/shared/edgejournal"
	"github.com/container-registry/harbor-satellite/internal/shared/testutil/pending"
	"github.com/stretchr/testify/require"
)

const (
	pendingJournal       = "journal ingest is a stub; the sync handler ignores uploaded journals"
	journalEpoch   int64 = 1760529600000
	satelliteName        = "edge-01"
)

func journalRecord(t *testing.T, seq int64, typ edgejournal.RecordType, payload any) fleet.JournalRecord {
	t.Helper()
	data, err := json.Marshal(payload)
	require.NoError(t, err)
	return fleet.JournalRecord{Seq: seq, Type: typ, RecordedAt: now, Payload: data}
}

func replicated(t *testing.T, seqs ...int64) []fleet.JournalRecord {
	t.Helper()
	out := make([]fleet.JournalRecord, 0, len(seqs))
	for _, seq := range seqs {
		out = append(out, journalRecord(t, seq, edgejournal.RecordReplication, edgejournal.Replication{
			SchemaVersion: edgejournal.SchemaVersion, Action: edgejournal.ActionReplicated,
			Group: "g1", Repository: "library/alpine", Tag: "3.20", Digest: "sha256:aaa",
		}))
	}
	return out
}

func seqRange(from, through int64) []int64 {
	var out []int64
	for s := from; s <= through; s++ {
		out = append(out, s)
	}
	return out
}

func newJournalService(t *testing.T, w *world) *fleet.JournalService {
	t.Helper()
	svc, err := fleet.NewJournalService(w.repo)
	require.NoError(t, err)
	return svc
}

func payloadOf(t *testing.T, e fleet.EdgeEvent) map[string]any {
	t.Helper()
	data, err := json.Marshal(e.Payload)
	require.NoError(t, err)
	var m map[string]any
	require.NoError(t, json.Unmarshal(data, &m), "%s payload %s", e.Kind, data)
	return m
}

func gapPayload(t *testing.T, e fleet.EdgeEvent) fleet.EdgeJournalGapPayload {
	t.Helper()
	require.Equal(t, fleet.EventEdgeJournalGap, e.Kind)
	data, err := json.Marshal(e.Payload)
	require.NoError(t, err)
	var gap fleet.EdgeJournalGapPayload
	require.NoError(t, json.Unmarshal(data, &gap))
	return gap
}

func seqOf(t *testing.T, e fleet.EdgeEvent) int64 {
	t.Helper()
	seq, ok := payloadOf(t, e)["seq"].(float64)
	require.True(t, ok, "%s has a seq", e.Kind)
	return int64(seq)
}

func TestJournalService_Ingest(t *testing.T) {
	type gap struct {
		from, through int64
		cause         string
	}
	cases := []struct {
		name    string
		cursor  fleet.JournalCursor
		after   int64
		seqs    []int64
		ack     int64
		gap     *gap
		emitted []int64
	}{
		{name: "acks the run above the cursor", seqs: []int64{1, 2, 3}, ack: 3, emitted: []int64{1, 2, 3}},
		{name: "a retried upload emits nothing", cursor: fleet.JournalCursor{AckedSeq: 3}, seqs: []int64{1, 2, 3}, ack: 3},
		{name: "an overlapping upload emits only the new records", cursor: fleet.JournalCursor{AckedSeq: 2}, seqs: []int64{1, 2, 3, 4}, ack: 4, emitted: []int64{3, 4}},
		{name: "a cursor behind the satellite's floor takes the floor", after: 500, seqs: []int64{501, 502}, ack: 502, gap: &gap{1, 500, fleet.GapCursorBehind}, emitted: []int64{501, 502}},
		{name: "a fresh cursor below the floor", cursor: fleet.JournalCursor{Fresh: true}, after: 500, seqs: []int64{501}, ack: 501, gap: &gap{1, 500, fleet.GapNewEpoch}, emitted: []int64{501}},
		{name: "a raised floor with nothing above it", cursor: fleet.JournalCursor{AckedSeq: 4}, after: 9, ack: 9, gap: &gap{5, 9, fleet.GapCursorBehind}},
		{name: "keeps the cursor when the satellite's floor is behind it", cursor: fleet.JournalCursor{AckedSeq: 10}, after: 5, seqs: seqRange(6, 12), ack: 12, emitted: []int64{11, 12}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			pending.Plan(t, "6.5", pendingJournal)
			w := newWorld()
			cursor := tc.cursor
			cursor.Epoch = journalEpoch
			w.repo.Journal.Cursor = cursor
			upload := fleet.JournalUpload{Epoch: journalEpoch, AfterSeq: tc.after, Records: replicated(t, tc.seqs...)}

			ack, err := newJournalService(t, w).Ingest(context.Background(), 7, satelliteName, upload)

			require.NoError(t, err)
			require.Equal(t, fleet.JournalAck{Epoch: journalEpoch, AckedSeq: tc.ack}, ack)
			require.Equal(t, []any{fleet.SatelliteID(7), journalEpoch}, onlyCall(t, w.rec, "Journal.OpenCursor").Args)
			if tc.ack == tc.cursor.AckedSeq {
				require.Zero(t, w.rec.Count("Journal.Advance"), "nothing new, no statement")
				return
			}
			advance := onlyCall(t, w.rec, "Journal.Advance")
			require.Equal(t, tc.ack, arg[int64](t, advance, 2))
			insideTx(t, w.rec, "Journal.Advance")
			events := w.repo.Journal.Advanced()
			if tc.gap != nil {
				require.NotEmpty(t, events)
				require.Equal(t, fleet.EdgeJournalGapPayload{Name: satelliteName, Epoch: journalEpoch, From: tc.gap.from, Through: tc.gap.through, Cause: tc.gap.cause},
					gapPayload(t, events[0]))
				events = events[1:]
			}
			var seqs []int64
			for _, e := range events {
				require.Equal(t, fleet.EventEdgeReplicated, e.Kind)
				seqs = append(seqs, seqOf(t, e))
			}
			require.Equal(t, tc.emitted, seqs, "one event per record above the base, in seq order")
		})
	}
}

// A128: a record Ground Control cannot map is acked and reported, so one
// record from a newer satellite never blocks the journal.
func TestJournalService_IngestReportsARecordItCannotMapAsAGap(t *testing.T) {
	pending.Plan(t, "6.5", pendingJournal)
	w := newWorld()
	w.repo.Journal.Cursor = fleet.JournalCursor{Epoch: journalEpoch}
	records := replicated(t, 1, 2, 3)
	records[1] = journalRecord(t, 2, "quota", map[string]any{"schema_version": "1"})

	ack, err := newJournalService(t, w).Ingest(context.Background(), 7, satelliteName,
		fleet.JournalUpload{Epoch: journalEpoch, Records: records})

	require.NoError(t, err)
	require.Equal(t, fleet.JournalAck{Epoch: journalEpoch, AckedSeq: 3}, ack)
	events := w.repo.Journal.Advanced()
	require.Len(t, events, 3)
	require.EqualValues(t, 1, seqOf(t, events[0]))
	gap := gapPayload(t, events[1])
	require.Equal(t, int64(2), gap.From)
	require.Equal(t, int64(2), gap.Through)
	require.True(t, strings.HasPrefix(gap.Cause, fleet.GapRejectedPrefix), gap.Cause)
	require.EqualValues(t, 3, seqOf(t, events[2]))
}

// Epochs are time-ordered (A78): a delayed upload of a journal the
// satellite has replaced emits nothing and learns the newer epoch.
func TestJournalService_IngestAnswersAnOlderEpochWithTheCursors(t *testing.T) {
	for name, stored := range map[string]fleet.JournalCursor{
		"newer epoch with acks": {Epoch: journalEpoch + 1, AckedSeq: 9},
		"fresh newer epoch":     {Epoch: journalEpoch + 60_000, AckedSeq: 0},
	} {
		t.Run(name, func(t *testing.T) {
			pending.Plan(t, "6.5", pendingJournal)
			w := newWorld()
			w.repo.Journal.Cursor = stored

			ack, err := newJournalService(t, w).Ingest(context.Background(), 7, satelliteName,
				fleet.JournalUpload{Epoch: journalEpoch, Records: replicated(t, 1, 2, 3)})

			require.NoError(t, err)
			require.Equal(t, fleet.JournalAck{Epoch: stored.Epoch}, ack, "the satellite continues as Epoch+1")
			require.Equal(t, 1, w.rec.Count("Journal.OpenCursor"))
			require.Zero(t, w.rec.Count("Journal.Advance"))
		})
	}
}

// A125, A128: only a malformed envelope is rejected, in the answer and
// never as an error of the heartbeat.
func TestJournalService_IngestRejectsAMalformedEnvelope(t *testing.T) {
	cases := []struct {
		name   string
		upload func(t *testing.T) fleet.JournalUpload
	}{
		{"epoch below 1", func(t *testing.T) fleet.JournalUpload {
			return fleet.JournalUpload{Epoch: 0, Records: replicated(t, 1)}
		}},
		{"negative floor", func(t *testing.T) fleet.JournalUpload {
			return fleet.JournalUpload{Epoch: journalEpoch, AfterSeq: -1, Records: replicated(t, 0)}
		}},
		{"too many records", func(t *testing.T) fleet.JournalUpload {
			return fleet.JournalUpload{Epoch: journalEpoch, Records: replicated(t, seqRange(1, fleet.MaxJournalRecords+1)...)}
		}},
		{"seq below 1", func(t *testing.T) fleet.JournalUpload {
			return fleet.JournalUpload{Epoch: journalEpoch, Records: replicated(t, 0, 1)}
		}},
		{"a missing seq", func(t *testing.T) fleet.JournalUpload {
			return fleet.JournalUpload{Epoch: journalEpoch, Records: replicated(t, 1, 2, 4)}
		}},
		{"out of order", func(t *testing.T) fleet.JournalUpload {
			return fleet.JournalUpload{Epoch: journalEpoch, Records: replicated(t, 2, 1, 3)}
		}},
		{"a repeated seq", func(t *testing.T) fleet.JournalUpload {
			return fleet.JournalUpload{Epoch: journalEpoch, Records: replicated(t, 1, 1, 2)}
		}},
		{"records not right after the floor", func(t *testing.T) fleet.JournalUpload {
			return fleet.JournalUpload{Epoch: journalEpoch, AfterSeq: 4, Records: replicated(t, 7, 8)}
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			pending.Plan(t, "6.5", pendingJournal)
			w := newWorld()
			upload := tc.upload(t)

			ack, err := newJournalService(t, w).Ingest(context.Background(), 7, satelliteName, upload)

			require.NoError(t, err)
			require.NotEmpty(t, ack.Error)
			require.Equal(t, fleet.JournalAck{Epoch: upload.Epoch, AckedSeq: upload.AfterSeq, Error: ack.Error}, ack, "no ack advance")
			require.Empty(t, w.rec.Methods(), "a rejected upload costs no statement")
		})
	}
}

func TestJournalService_IngestRollsBackWhenAdvanceFails(t *testing.T) {
	pending.Plan(t, "6.5", pendingJournal)
	w := newWorld()
	w.repo.Journal.Cursor = fleet.JournalCursor{Epoch: journalEpoch}
	boom := errors.New("insert failed")
	w.rec.FailOn("Journal.Advance", boom)

	_, err := newJournalService(t, w).Ingest(context.Background(), 7, satelliteName,
		fleet.JournalUpload{Epoch: journalEpoch, Records: replicated(t, 1)})

	require.ErrorIs(t, err, boom)
	require.Equal(t, 1, w.rec.Count("Rollback"))
	require.Zero(t, w.rec.Count("Commit"))
}

func TestNewJournalService_NeedsARepository(t *testing.T) {
	pending.Plan(t, "6.5", pendingJournal)
	_, err := fleet.NewJournalService(nil)
	require.ErrorIs(t, err, fleet.ErrInvalid)
}

func TestNewEdgeEvent_MapsRecords(t *testing.T) {
	replication := func(action edgejournal.ReplicationAction, errText string) edgejournal.Replication {
		return edgejournal.Replication{
			SchemaVersion: edgejournal.SchemaVersion, CycleID: "c-1", Action: action, Group: "g1",
			Repository: "library/alpine", Tag: "3.20", Digest: "sha256:aaa", Bytes: 2048, DurationMs: 15, Error: errText,
		}
	}
	cases := []struct {
		name    string
		typ     edgejournal.RecordType
		payload any
		kind    fleet.EventKind
	}{
		{"replicated", edgejournal.RecordReplication, replication(edgejournal.ActionReplicated, ""), fleet.EventEdgeReplicated},
		{"deleted", edgejournal.RecordReplication, replication(edgejournal.ActionDeleted, ""), fleet.EventEdgeDeleted},
		{"replication failed", edgejournal.RecordReplication, replication(edgejournal.ActionReplicated, "manifest unknown"), fleet.EventEdgeArtifactFailed},
		{"deletion failed", edgejournal.RecordReplication, replication(edgejournal.ActionDeleted, "permission denied"), fleet.EventEdgeArtifactFailed},
		{"cri", edgejournal.RecordCRI, edgejournal.CRI{SchemaVersion: edgejournal.SchemaVersion, Runtime: "containerd", Success: true}, fleet.EventEdgeCRIConfigured},
		{"pull-through", edgejournal.RecordPullThrough, edgejournal.PullThrough{
			SchemaVersion: edgejournal.SchemaVersion, Repository: "library/alpine", Reference: "3.20", Digest: "sha256:aaa", Bytes: 4096,
		}, fleet.EventEdgePullThrough},
		{"unknown payload fields pass through", edgejournal.RecordReplication, map[string]any{
			"schema_version": "1", "action": "replicated", "group": "g1", "repository": "library/alpine",
			"tag": "3.20", "digest": "sha256:aaa", "platform": "linux/arm64",
		}, fleet.EventEdgeReplicated},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			pending.Plan(t, "9.4", "journal records are not mapped to edge events")
			record := journalRecord(t, 5, tc.typ, tc.payload)

			event, err := fleet.NewEdgeEvent(satelliteName, journalEpoch, record)

			require.NoError(t, err)
			require.Equal(t, tc.kind, event.Kind)
			want := map[string]any{}
			require.NoError(t, json.Unmarshal(record.Payload, &want))
			origin, err := json.Marshal(fleet.EdgeOrigin{Name: satelliteName, Epoch: journalEpoch, Seq: 5, RecordedAt: now})
			require.NoError(t, err)
			require.NoError(t, json.Unmarshal(origin, &want))
			require.Equal(t, want, payloadOf(t, event), "the record's fields and the origin at one level")
		})
	}
}

// A127: records dropped under backpressure arrive as one counted record.
func TestNewEdgeEvent_DroppedRecordsAreAGap(t *testing.T) {
	pending.Plan(t, "9.4", "journal records are not mapped to edge events")
	record := journalRecord(t, 9, edgejournal.RecordDropped, edgejournal.Dropped{SchemaVersion: edgejournal.SchemaVersion, Count: 8})

	event, err := fleet.NewEdgeEvent(satelliteName, journalEpoch, record)

	require.NoError(t, err)
	require.Equal(t, fleet.EdgeJournalGapPayload{Name: satelliteName, Epoch: journalEpoch, From: 9, Through: 9, Cause: fleet.GapDropped, Dropped: 8},
		gapPayload(t, event))
}

func TestNewEdgeEvent_RejectsMalformedRecords(t *testing.T) {
	cases := []struct {
		name   string
		record fleet.JournalRecord
	}{
		{"unknown type", fleet.JournalRecord{Seq: 1, Type: "report", Payload: json.RawMessage(`{}`)}},
		{"payload not an object", fleet.JournalRecord{Seq: 1, Type: edgejournal.RecordCRI, Payload: json.RawMessage(`[]`)}},
		{"payload not JSON", fleet.JournalRecord{Seq: 1, Type: edgejournal.RecordCRI, Payload: json.RawMessage(`{`)}},
		{"unknown replication action", fleet.JournalRecord{Seq: 1, Type: edgejournal.RecordReplication, Payload: json.RawMessage(`{"action":"moved"}`)}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			pending.Plan(t, "9.4", "journal records are not mapped to edge events")
			_, err := fleet.NewEdgeEvent(satelliteName, journalEpoch, tc.record)
			require.ErrorIs(t, err, fleet.ErrInvalid)
		})
	}
}

func TestEdgeEventKindsAreLogOnly(t *testing.T) {
	for _, k := range []fleet.EventKind{
		fleet.EventEdgeReplicated, fleet.EventEdgeDeleted, fleet.EventEdgeArtifactFailed,
		fleet.EventEdgePullThrough, fleet.EventEdgeCRIConfigured, fleet.EventEdgeJournalGap,
	} {
		require.True(t, k.LogOnly(), "%s is appended already done", k)
	}
}

// Edge payloads join the event catalogue; this freezes the JSON Ground
// Control adds.
func TestEdgePayloads_JSON(t *testing.T) {
	for name, tc := range map[string]struct {
		payload any
		want    string
	}{
		"origin": {
			fleet.EdgeOrigin{Name: satelliteName, Epoch: journalEpoch, Seq: 5, RecordedAt: time.Date(2026, 10, 15, 12, 0, 0, 0, time.UTC)},
			`{"name":"edge-01","epoch":1760529600000,"seq":5,"recorded_at":"2026-10-15T12:00:00Z"}`,
		},
		"replication with origin": {
			fleet.EdgeReplicationPayload{
				Replication: edgejournal.Replication{SchemaVersion: edgejournal.SchemaVersion, Action: edgejournal.ActionReplicated, Group: "g1", Repository: "library/alpine", Tag: "3.20", Digest: "sha256:abc"},
				EdgeOrigin:  fleet.EdgeOrigin{Name: satelliteName, Epoch: journalEpoch, Seq: 5, RecordedAt: time.Date(2026, 10, 15, 12, 0, 0, 0, time.UTC)},
			},
			`{"schema_version":"` + edgejournal.SchemaVersion + `","action":"replicated","group":"g1","repository":"library/alpine","tag":"3.20","digest":"sha256:abc","name":"edge-01","epoch":1760529600000,"seq":5,"recorded_at":"2026-10-15T12:00:00Z"}`,
		},
		"gap": {
			fleet.EdgeJournalGapPayload{Name: satelliteName, Epoch: journalEpoch, From: 1, Through: 500, Cause: fleet.GapCursorBehind},
			`{"name":"edge-01","epoch":1760529600000,"from":1,"through":500,"cause":"cursor_behind"}`,
		},
		"dropped": {
			fleet.EdgeJournalGapPayload{Name: satelliteName, Epoch: journalEpoch, From: 9, Through: 9, Cause: fleet.GapDropped, Dropped: 8},
			`{"name":"edge-01","epoch":1760529600000,"from":9,"through":9,"cause":"dropped","dropped":8}`,
		},
	} {
		got, err := json.Marshal(tc.payload)
		require.NoError(t, err, name)
		require.Equal(t, tc.want, string(got), name)
	}
}
