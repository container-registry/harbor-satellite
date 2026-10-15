package state

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/container-registry/harbor-satellite/internal/satellite/events"
	"github.com/container-registry/harbor-satellite/internal/satellite/journal"
	"github.com/container-registry/harbor-satellite/internal/satellite/scheduler"
	"github.com/container-registry/harbor-satellite/internal/satellite/store"
	"github.com/container-registry/harbor-satellite/internal/shared/edgejournal"
	"github.com/container-registry/harbor-satellite/internal/shared/testutil/pending"
	"github.com/container-registry/harbor-satellite/pkg/config"
	"github.com/container-registry/harbor-satellite/pkg/groundcontrol"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/require"
)

// digestFetcher resolves a fixed digest and counts pulls.
type digestFetcher struct {
	digest string
	err    error
	pulls  int
}

func (f *digestFetcher) FetchStateArtifact(context.Context, any, *zerolog.Logger) error {
	f.pulls++
	return nil
}

func (f *digestFetcher) FetchDigest(context.Context, *zerolog.Logger) (string, error) {
	return f.digest, f.err
}

func TestStateArtifactChanged(t *testing.T) {
	pending.Plan(t, "6.1", "satellite and group states are pulled on every tick")
	ctx := context.Background()
	nop := zerolog.Nop()
	fetcher := &digestFetcher{digest: "sha256:aaa"}

	digest, changed, err := StateArtifactChanged(ctx, fetcher, "sha256:aaa", &nop)
	require.NoError(t, err)
	require.False(t, changed)
	require.Equal(t, "sha256:aaa", digest)
	digest, changed, err = StateArtifactChanged(ctx, fetcher, "sha256:old", &nop)
	require.NoError(t, err)
	require.True(t, changed)
	require.Equal(t, "sha256:aaa", digest)
	require.Zero(t, fetcher.pulls, "only the digest is resolved")

	boom := errors.New("registry down")
	fetcher.err = boom
	_, _, err = StateArtifactChanged(ctx, fetcher, "sha256:aaa", &nop)
	require.ErrorIs(t, err, boom)
}

func TestExecute_UnchangedStateCostsOneHeadPerArtifact(t *testing.T) {
	pending.Plan(t, "6.1", "satellite and group states are pulled on every tick")
	reg := newCountingRegistry(t)
	app := pushImage(t, reg, "team/app", []byte("layer"))
	sat := newColdPathSatellite(t, reg, config.AppConfig{}, map[string][]Artifact{"edge": {app}})
	require.NoError(t, sat.process.Execute(testContext()))
	reg.reset()

	require.NoError(t, sat.process.Execute(testContext()))

	for _, repo := range []string{sat.satelliteStateRepo, sat.groupStateRepos["edge"], sat.configStateRepo} {
		require.Equal(t, 1, reg.count(http.MethodHead, repo+"/manifests/"), "HEAD %s", repo)
		require.Zero(t, reg.count(http.MethodGet, repo+"/manifests/"), "GET %s", repo)
	}
	require.Zero(t, reg.count(http.MethodGet, "/blobs/"), "no blob is downloaded")
}

func TestReplication_ConcurrencyBoundedAcrossGroups(t *testing.T) {
	pending.Plan(t, "6.2", "every group replicates in its own goroutine")
	reg := newCountingRegistry(t)
	groups := map[string][]Artifact{}
	for i := range 4 {
		name := fmt.Sprintf("g%d", i)
		groups[name] = []Artifact{pushImage(t, reg, "team/app"+name, []byte("layer "+name))}
	}
	sat := newColdPathSatellite(t, reg, config.AppConfig{ReplicationConcurrency: 2}, groups)
	local := &blockingStore{release: make(chan struct{})}
	var once sync.Once
	release := func() { once.Do(func() { close(local.release) }) }
	t.Cleanup(release)
	sat.process.SetStores(local, nil)
	done := make(chan error, 1)
	go func() { done <- sat.process.Execute(testContext()) }()

	require.Eventually(t, func() bool {
		running, _ := local.counts()
		return running >= 2
	}, 10*time.Second, 10*time.Millisecond)
	time.Sleep(200 * time.Millisecond)
	running, peak := local.counts()
	release()
	require.NoError(t, <-done)

	require.Equal(t, 2, running, "replications in flight with replication_concurrency 2")
	require.Equal(t, 2, peak)
}

func TestReplication_BandwidthCapStaysUnderCap(t *testing.T) {
	pending.Plan(t, "6.2", "replication has no source of its own and no bandwidth budget")
	reg := newCountingRegistry(t)
	big := pushImage(t, reg, "team/big", bytes.Repeat([]byte("satellite"), (128<<10)/9))
	_, probeLayer := pushRetagTestImage(t, reg.host, "team/probe", "latest", bytes.Repeat([]byte("proxy"), (128<<10)/5))
	sat := newColdPathSatellite(t, reg, config.AppConfig{ReplicationBandwidthBytesPerSec: 64 << 10}, map[string][]Artifact{"edge": {big}})
	local, err := store.NewOCIStore(t.TempDir())
	require.NoError(t, err)
	source, err := store.ResolveSourceRegistry(sat.cm)
	require.NoError(t, err)
	// cmd/satellite shares this Harbor store between the proxy and
	// replication today.
	shared, err := store.NewRegistryStore(source)
	require.NoError(t, err)
	replicationSource, err := store.NewRegistryStoreWithOptions(func() (store.RegistryOptions, error) {
		opts, err := store.ResolveSourceRegistry(sat.cm)
		opts.BandwidthBytesPerSec = 64 << 10
		return opts, err
	})
	require.NoError(t, err)
	sat.process.SetStores(local, shared)
	sat.process.SetReplicationSource(replicationSource)

	start := time.Now()
	done := make(chan error, 1)
	go func() { done <- sat.process.Execute(testContext()) }()
	time.Sleep(100 * time.Millisecond)
	fetchStart := time.Now()
	resp, err := shared.Fetch(context.Background(), store.Artifact{Name: "team/probe", Digest: probeLayer.Digest.String()}, probeLayer, store.PullResourceBlob, nil)
	require.NoError(t, err)
	_, err = io.Copy(io.Discard, resp.Body)
	require.NoError(t, err)
	require.NoError(t, resp.Body.Close())
	fetched := time.Since(fetchStart)
	require.NoError(t, <-done)
	elapsed := time.Since(start)

	require.GreaterOrEqual(t, elapsed, 900*time.Millisecond, "128 KiB at 64 KiB/s took %s", elapsed)
	require.Less(t, fetched, 300*time.Millisecond, "the proxy's 128 KiB fetch is not throttled")
}

// replicationRecords returns the replication payloads the journal holds.
func replicationRecords(t *testing.T, j *journal.Journal) []edgejournal.Replication {
	t.Helper()
	records, err := j.ReadFrom(0, 100)
	require.NoError(t, err)
	var out []edgejournal.Replication
	for _, r := range records {
		if r.Type != edgejournal.RecordReplication {
			continue
		}
		var p edgejournal.Replication
		require.NoError(t, json.Unmarshal(r.Payload, &p))
		require.Equal(t, edgejournal.SchemaVersion, p.SchemaVersion)
		p.SchemaVersion, p.CycleID, p.Bytes, p.DurationMs = "", "", 0, 0
		out = append(out, p)
	}
	return out
}

func TestExecute_JournalsReplicationPerArtifact(t *testing.T) {
	pending.Plan(t, "6.4", "replication results are not journaled")
	reg := newCountingRegistry(t)
	one := pushImage(t, reg, "team/one", []byte("layer one"))
	two := pushImage(t, reg, "team/two", []byte("layer two"))
	sat := newColdPathSatellite(t, reg, config.AppConfig{}, map[string][]Artifact{"edge": {one, two}})
	j, err := journal.Open(journal.Options{Dir: t.TempDir()})
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, j.Close()) })
	sat.process.SetJournal(j)
	replicated := func(a Artifact, action edgejournal.ReplicationAction) edgejournal.Replication {
		return edgejournal.Replication{Action: action, Group: "edge", Repository: a.Repository, Tag: "latest", Digest: a.Digest}
	}

	require.NoError(t, sat.process.Execute(testContext()))
	require.ElementsMatch(t, []edgejournal.Replication{
		replicated(one, edgejournal.ActionReplicated),
		replicated(two, edgejournal.ActionReplicated),
	}, replicationRecords(t, j))

	pushStateArtifact(t, sat.groupStates["edge"], State{Registry: reg.URL, Artifacts: []Artifact{one}})
	require.NoError(t, sat.process.Execute(testContext()))
	require.Contains(t, replicationRecords(t, j), replicated(two, edgejournal.ActionDeleted), "deletions are journaled too")
}

func TestLastRun_RecordsDigestsAndDuration(t *testing.T) {
	pending.Plan(t, "6.6", "the report's digests and sync duration are never set")
	reg := newCountingRegistry(t)
	app := pushImage(t, reg, "team/app", []byte("layer"))
	tool := pushImage(t, reg, "team/tool", []byte("tool layer"))
	sat := newColdPathSatellite(t, reg, config.AppConfig{}, map[string][]Artifact{"edge": {app}, "tools": {tool}})
	applied := func(group string) AppliedState {
		return AppliedState{Ref: "http://" + sat.groupStates[group], Digest: sat.groupStateDigests[group]}
	}
	require.Zero(t, sat.process.LastRun())

	require.NoError(t, sat.process.Execute(testContext()))

	run := sat.process.LastRun()
	require.Equal(t, sat.satelliteStateDigest, run.StateDigest)
	require.Equal(t, sat.configStateDigest, run.ConfigDigest)
	require.ElementsMatch(t, []AppliedState{applied("edge"), applied("tools")}, run.Applied)
	require.Positive(t, run.Duration)
	require.Empty(t, run.LastReplicationError)
	require.False(t, run.FinishedAt.IsZero())

	pushStateArtifact(t, sat.groupStates["tools"], State{Registry: reg.URL, Artifacts: []Artifact{{
		Repository: "team/missing", Tags: []string{"latest"}, Digest: "sha256:" + strings.Repeat("0", 64), Type: "image",
	}}})
	require.Error(t, sat.process.Execute(testContext()))
	failed := sat.process.LastRun()
	require.NotEmpty(t, failed.LastReplicationError)
	require.False(t, failed.FinishedAt.Before(run.FinishedAt))
	require.ElementsMatch(t, []AppliedState{applied("edge"), applied("tools")}, failed.Applied,
		"the unchanged group keeps its digest and the failed one the digest it applied before")
}

type fixedRun struct{ run RunSummary }

func (f fixedRun) LastRun() RunSummary { return f.run }

func TestStatusReport_SetsDigestsAndDuration(t *testing.T) {
	pending.Plan(t, "6.6", "the report's digests and sync duration are never set")
	var received groundcontrol.SatelliteStatusRequest
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.NoError(t, json.NewDecoder(r.Body).Decode(&received))
		writeJSON(t, w, groundcontrol.SatelliteSyncResponse{Events: []string{}})
	}))
	t.Cleanup(srv.Close)
	p := NewStatusReportingProcess(newReportingTestCM(t, srv.URL), nil)
	p.SetRunSummarySource(fixedRun{RunSummary{
		StateDigest:          "sha256:aaa",
		ConfigDigest:         "sha256:bbb",
		Applied:              []AppliedState{{Ref: "http://harbor/satellite/group-state/edge/state:latest", Digest: "sha256:ccc"}},
		Duration:             1500 * time.Millisecond,
		LastReplicationError: "replicate team/app: manifest unknown",
	}})

	require.NoError(t, p.Execute(testContext()))

	require.Equal(t, "sha256:aaa", received.LatestStateDigest)
	require.Equal(t, "sha256:bbb", received.LatestConfigDigest)
	require.Equal(t, []groundcontrol.AppliedState{{Ref: "http://harbor/satellite/group-state/edge/state:latest", Digest: "sha256:ccc"}}, received.AppliedStates)
	require.EqualValues(t, 1500, received.LastSyncDurationMs)
	require.Equal(t, "replicate team/app: manifest unknown", received.LastReplicationError)
}

// seenUpload is one journal upload as Ground Control received it.
type seenUpload struct {
	epoch, after int64
	seqs         []int64
}

// journalGC is a Ground Control that records every journal upload and
// answers it with answer; a nil answer omits the response's journal, as a
// Ground Control without journal support does.
func journalGC(t *testing.T, answer func(groundcontrol.JournalUpload) *groundcontrol.JournalAck) (url string, uploads func() []seenUpload) {
	t.Helper()
	var (
		mu   sync.Mutex
		seen []seenUpload
	)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req groundcontrol.SatelliteStatusRequest
		require.NoError(t, json.NewDecoder(r.Body).Decode(&req))
		resp := groundcontrol.SatelliteSyncResponse{Events: []string{}}
		upload := seenUpload{}
		if req.Journal.Epoch != 0 {
			upload = seenUpload{epoch: req.Journal.Epoch, after: req.Journal.AfterSeq}
			for _, rec := range req.Journal.Records {
				upload.seqs = append(upload.seqs, rec.Seq)
			}
			resp.Journal = answer(req.Journal)
		}
		mu.Lock()
		seen = append(seen, upload)
		mu.Unlock()
		writeJSON(t, w, resp)
	}))
	t.Cleanup(srv.Close)
	return srv.URL, func() []seenUpload {
		mu.Lock()
		defer mu.Unlock()
		return append([]seenUpload(nil), seen...)
	}
}

// ackAll acknowledges every upload up to its last record.
func ackAll(u groundcontrol.JournalUpload) *groundcontrol.JournalAck {
	ack := &groundcontrol.JournalAck{Epoch: u.Epoch, AckedSeq: u.AfterSeq}
	if n := len(u.Records); n > 0 {
		ack.AckedSeq = u.Records[n-1].Seq
	}
	return ack
}

// journalReport is a status report process with a journal of n records,
// talking to gcURL with upload_batch batch.
func journalReport(t *testing.T, gcURL string, batch, n int) (*StatusReportingProcess, *journal.Journal) {
	t.Helper()
	j, err := journal.Open(journal.Options{Dir: t.TempDir()})
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, j.Close()) })
	for range n {
		_, err := j.Append(context.Background(), journal.Record{Type: edgejournal.RecordCRI, RecordedAt: time.Now(), Payload: json.RawMessage(`{"schema_version":"1"}`)})
		require.NoError(t, err)
	}
	dir := t.TempDir()
	cm, err := config.NewConfigManager(filepath.Join(dir, "config.json"), filepath.Join(dir, "prev.json"), "token", gcURL, false, &config.Config{
		StateConfig: config.StateConfig{StateURL: "http://registry/satellite/satellite-state/test-sat/state:latest"},
		AppConfig: config.AppConfig{
			GroundControlURL:  config.URL(gcURL),
			HeartbeatInterval: "@every 30s",
			UseUnsecure:       true,
			Journal:           config.JournalConfig{UploadBatch: batch},
		},
	})
	require.NoError(t, err)
	p := NewStatusReportingProcess(cm, nil)
	p.SetJournal(j)
	return p, j
}

func executeTimes(t *testing.T, p *StatusReportingProcess, n int) {
	t.Helper()
	for range n {
		require.NoError(t, p.Execute(testContext()))
	}
}

func TestStatusReport_UploadsJournalAndAcks(t *testing.T) {
	pending.Plan(t, "6.5", "the heartbeat carries no journal")
	url, uploads := journalGC(t, ackAll)
	p, j := journalReport(t, url, 2, 3)

	executeTimes(t, p, 3)

	e := j.Epoch()
	require.Equal(t, []seenUpload{
		{epoch: e, after: 0, seqs: []int64{1}},
		{epoch: e, after: 1, seqs: []int64{2, 3}},
		{},
	}, uploads(), "one record until Ground Control has answered a journal, then upload_batch, then nothing")
	require.EqualValues(t, 3, j.Acked())
}

func TestStatusReport_SendsOneRecordWhileGroundControlIgnoresTheJournal(t *testing.T) {
	pending.Plan(t, "6.5", "the heartbeat carries no journal")
	url, uploads := journalGC(t, func(groundcontrol.JournalUpload) *groundcontrol.JournalAck { return nil })
	p, j := journalReport(t, url, 2, 3)

	executeTimes(t, p, 3)

	e := j.Epoch()
	one := seenUpload{epoch: e, after: 0, seqs: []int64{1}}
	require.Equal(t, []seenUpload{one, one, one}, uploads())
	require.Zero(t, j.Acked())
}

func TestStatusReport_ContinuesAboveANewerGroundControlEpoch(t *testing.T) {
	pending.Plan(t, "6.5", "the heartbeat carries no journal")
	var gcEpoch atomic.Int64
	url, uploads := journalGC(t, func(u groundcontrol.JournalUpload) *groundcontrol.JournalAck {
		gcEpoch.CompareAndSwap(0, u.Epoch+60_000)
		if u.Epoch < gcEpoch.Load() {
			return &groundcontrol.JournalAck{Epoch: gcEpoch.Load()}
		}
		return ackAll(u)
	})
	p, j := journalReport(t, url, 2, 3)

	executeTimes(t, p, 2)

	got := uploads()
	require.Len(t, got, 2)
	require.Equal(t, gcEpoch.Load()+1, got[1].epoch, "the journal continues as Ground Control's epoch + 1")
	require.Equal(t, []int64{1, 2}, got[1].seqs, "and uploads its records again")
	require.Equal(t, gcEpoch.Load()+1, j.Epoch())
}

// A135: an upload_batch above the sync contract's limit is capped.
func TestStatusReport_CapsTheBatchAtTheSyncLimit(t *testing.T) {
	pending.Plan(t, "6.5", "the heartbeat carries no journal")
	url, uploads := journalGC(t, ackAll)
	p, j := journalReport(t, url, 5000, config.MaxJournalUploadBatch+2)

	executeTimes(t, p, 2)

	got := uploads()
	require.Len(t, got, 2)
	require.Len(t, got[0].seqs, 1, "one record until Ground Control has answered a journal")
	require.Len(t, got[1].seqs, config.MaxJournalUploadBatch)
	require.EqualValues(t, config.MaxJournalUploadBatch+1, j.Acked())
}

// A125: a batch Ground Control keeps rejecting must not block the journal.
func TestStatusReport_DropsABatchRejectedThreeTimes(t *testing.T) {
	pending.Plan(t, "6.5", "the heartbeat carries no journal")
	url, uploads := journalGC(t, func(u groundcontrol.JournalUpload) *groundcontrol.JournalAck {
		if len(u.Records) > 0 && u.Records[0].Seq == 1 {
			return &groundcontrol.JournalAck{Epoch: u.Epoch, AckedSeq: u.AfterSeq, Error: "invalid journal: payload is not an object"}
		}
		return ackAll(u)
	})
	p, j := journalReport(t, url, 1, 2)

	executeTimes(t, p, 4)

	e := j.Epoch()
	rejected := seenUpload{epoch: e, after: 0, seqs: []int64{1}}
	require.Equal(t, []seenUpload{rejected, rejected, rejected, {epoch: e, after: 1, seqs: []int64{2}}}, uploads(),
		"after the third rejection the floor rises past the batch")
	require.EqualValues(t, 2, j.Acked())
}

// idleProcess is an event process that returns at once.
type idleProcess struct{ name string }

func (p idleProcess) Name() string                  { return p.name }
func (p idleProcess) Execute(context.Context) error { return nil }
func (p idleProcess) IsRunning() bool               { return false }
func (p idleProcess) IsComplete() bool              { return false }

func TestReportingProcess_EventDoesNotBlockHeartbeat(t *testing.T) {
	pending.Plan(t, "6.7", "SendEvent blocks the heartbeat until the event loop receives, forever once it has stopped")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(t, w, groundcontrol.SatelliteSyncResponse{Events: []string{"refresh_credentials"}})
	}))
	t.Cleanup(srv.Close)
	log := zerolog.Nop()
	sched, err := scheduler.NewScheduler(idleProcess{name: "refresh_credentials"}, &log)
	require.NoError(t, err)
	es := events.NewEventScheduler(&log)
	stopped, cancel := context.WithCancel(context.Background())
	cancel()
	es.Register(stopped, sched)
	require.NoError(t, sched.Stop(context.Background()))
	p := NewStatusReportingProcess(newReportingTestCM(t, srv.URL), es)

	done := make(chan error, 1)
	go func() { done <- p.Execute(testContext()) }()
	select {
	case err := <-done:
		require.NoError(t, err)
	case <-time.After(2 * time.Second):
		t.Fatal("the heartbeat blocked on the refresh_credentials event")
	}
}
