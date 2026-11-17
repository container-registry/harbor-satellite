package state

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/container-registry/harbor-satellite/internal/satellite/journal"
	"github.com/container-registry/harbor-satellite/internal/satellite/telemetry"
	"github.com/container-registry/harbor-satellite/internal/satellite/telemetry/telemetrytest"
	"github.com/container-registry/harbor-satellite/internal/shared/edgejournal"
	"github.com/container-registry/harbor-satellite/pkg/config"
	"github.com/container-registry/harbor-satellite/pkg/groundcontrol"
	"github.com/stretchr/testify/require"
)

// beat is what a size-limited Ground Control saw of one heartbeat.
type beat struct {
	status    int
	records   int
	journal   bool
	inventory bool
}

// limitedGC answers like Ground Control's /satellites/sync behind
// SYNC_MAX_BODY_BYTES: a body above limit is refused with 413, any other
// journal is acknowledged.
func limitedGC(t *testing.T, limit int64) (string, func() []beat) {
	t.Helper()
	var (
		mu   sync.Mutex
		seen []beat
	)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		r.Body = http.MaxBytesReader(w, r.Body, limit)
		var raw map[string]json.RawMessage
		err := json.NewDecoder(r.Body).Decode(&raw)
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			mu.Lock()
			seen = append(seen, beat{status: http.StatusRequestEntityTooLarge})
			mu.Unlock()
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusRequestEntityTooLarge)
			_, werr := w.Write([]byte(`{"code":413,"message":"request body too large"}`))
			require.NoError(t, werr)
			return
		}
		require.NoError(t, err)
		var req groundcontrol.SatelliteStatusRequest
		body, err := json.Marshal(raw)
		require.NoError(t, err)
		require.NoError(t, json.Unmarshal(body, &req))
		_, inventory := raw["inventory_digest"]
		b := beat{status: http.StatusOK, records: len(req.Journal.Records), journal: req.Journal.Epoch != 0, inventory: inventory}
		resp := groundcontrol.SatelliteSyncResponse{Events: []string{}}
		if b.journal {
			resp.Journal = ackAll(req.Journal)
		}
		mu.Lock()
		seen = append(seen, b)
		mu.Unlock()
		writeJSON(t, w, resp)
	}))
	t.Cleanup(srv.Close)
	return srv.URL, func() []beat {
		mu.Lock()
		defer mu.Unlock()
		return append([]beat(nil), seen...)
	}
}

// bigRecordsReport is a report process whose journal holds n replication
// records with an error of errBytes, and whose BYO registry has an empty
// catalog, so every beat carries an inventory digest.
func bigRecordsReport(t *testing.T, gcURL string, batch, n, errBytes int) *StatusReportingProcess {
	t.Helper()
	j, err := journal.Open(journal.Options{Dir: t.TempDir()})
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, j.Close()) })
	for range n {
		payload, merr := json.Marshal(edgejournal.Replication{SchemaVersion: edgejournal.SchemaVersion, Action: edgejournal.ActionReplicated, Group: "g", Repository: "library/app", Tag: "v1", Error: strings.Repeat("e", errBytes)})
		require.NoError(t, merr)
		_, err = j.Append(context.Background(), journal.Record{Type: edgejournal.RecordReplication, RecordedAt: time.Now(), Payload: payload})
		require.NoError(t, err)
	}
	registry := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeJSON(t, w, map[string]any{"repositories": []string{}})
	}))
	t.Cleanup(registry.Close)
	dir := t.TempDir()
	cm, err := config.NewConfigManager(filepath.Join(dir, "config.json"), filepath.Join(dir, "prev.json"), "token", gcURL, false, &config.Config{
		StateConfig: config.StateConfig{StateURL: "http://registry/satellite/satellite-state/test-sat/state:latest"},
		AppConfig: config.AppConfig{
			GroundControlURL:         config.URL(gcURL),
			HeartbeatInterval:        "@every 30s",
			UseUnsecure:              true,
			BringOwnRegistry:         true,
			LocalRegistryCredentials: config.RegistryCredentials{URL: config.URL(registry.URL)},
			Journal:                  config.JournalConfig{UploadBatch: batch},
		},
	})
	require.NoError(t, err)
	p := NewStatusReportingProcess(cm, nil)
	p.SetJournal(j)
	return p
}

// executeAll runs n heartbeats and returns how many failed.
func executeAll(p *StatusReportingProcess, n int) int {
	failed := 0
	for range n {
		if err := p.Execute(testContext()); err != nil {
			failed++
		}
	}
	return failed
}

// A 413 halves the journal batch for the following beats; the cap grows
// back after growAfter passing beats.
func TestStatusReport_HalvesTheBatchOn413(t *testing.T) {
	url, beats := limitedGC(t, 3000)
	p := bigRecordsReport(t, url, 8, 40, 900)
	spy := &telemetrytest.Spy{}
	p.SetRecorder(spy)

	require.Equal(t, 3, executeAll(p, 9))

	var records, statuses []int
	for _, b := range beats() {
		records = append(records, b.records)
		statuses = append(statuses, b.status)
	}
	require.Equal(t, []int{200, 413, 413, 200, 200, 200, 200, 413, 200}, statuses)
	got := spy.Snapshot()
	require.Equal(t, []string{"200", "413", "413", "200", "200", "200", "200", "413", "200"}, got.Heartbeats, "413s show in satellite_heartbeats_total")
	require.Equal(t, []string{telemetry.TooLargeHalvedBatch, telemetry.TooLargeHalvedBatch, telemetry.TooLargeHalvedBatch}, got.TooLarge)
	accepted := 0
	for i, s := range statuses {
		if s == http.StatusOK {
			accepted += records[i]
		}
	}
	require.Equal(t, 1+2*5, accepted, "one record before the first answer, then 2 per beat after 8 and 4 were refused")
}

// When one record is still too large the beat goes without the journal
// and the inventory, so Ground Control keeps seeing the satellite.
func TestStatusReport_SendsABareBeatWhenOneRecordIsTooLarge(t *testing.T) {
	url, beats := limitedGC(t, 1100)
	p := bigRecordsReport(t, url, 8, 4, 1000)
	spy := &telemetrytest.Spy{}
	p.SetRecorder(spy)
	bare := beat{status: http.StatusOK}
	refused := beat{status: http.StatusRequestEntityTooLarge}

	require.Equal(t, 2, executeAll(p, 4))

	require.Equal(t, []beat{refused, bare, refused, bare}, beats())
	got := spy.Snapshot()
	require.Equal(t, []string{"413", "200", "413", "200"}, got.Heartbeats)
	require.Equal(t, []string{telemetry.TooLargeWithoutJournal, telemetry.TooLargeWithoutJournal}, got.TooLarge)
}

func TestOversizeState_LiftsTheCapOnceTheBacklogDrained(t *testing.T) {
	var u journalUploader
	u.oversize = oversizeState{limit: 4, passed: 2}
	u.delivered(&groundcontrol.JournalUpload{Records: make([]groundcontrol.JournalRecord, 3)}, 500)
	require.Equal(t, oversizeState{}, u.oversize)

	u.oversize = oversizeState{limit: 256, passed: growAfter - 1}
	u.delivered(&groundcontrol.JournalUpload{Records: make([]groundcontrol.JournalRecord, 256)}, 500)
	require.Equal(t, oversizeState{}, u.oversize, "doubling reaches upload_batch, so the cap is lifted")

	u.oversize = oversizeState{bare: true}
	require.Equal(t, telemetry.TooLargeNone, u.tooLarge(nil), "a refused bare beat has nothing left to cut")
	u.delivered(nil, 500)
	require.Equal(t, oversizeState{limit: 1}, u.oversize, "after a bare beat passes, one record is tried again")
}

func TestApplyRunSummary_BoundsTheReplicationError(t *testing.T) {
	var req StatusReportParams
	applyRunSummary(&req, RunSummary{LastReplicationError: strings.Repeat("x", 1<<20)})
	require.Len(t, req.LastReplicationError, edgejournal.MaxStringBytes)
}
