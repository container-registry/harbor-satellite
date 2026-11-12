package journal_test

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/container-registry/harbor-satellite/internal/satellite/journal"
	"github.com/container-registry/harbor-satellite/internal/shared/edgejournal"
	"github.com/container-registry/harbor-satellite/internal/shared/testutil/pending"
	"github.com/stretchr/testify/require"
)

const pendingJournal = "the edge journal is a stub"

// killChildDir makes the test binary act as the process TestJournal_SurvivesKill9 kills.
const killChildDir = "JOURNAL_TEST_KILL_CHILD_DIR"

// killAfter is how many records the child appends before it waits to be
// killed.
const killAfter = 50

func TestMain(m *testing.M) {
	if dir := os.Getenv(killChildDir); dir != "" {
		fmt.Fprintln(os.Stderr, appendThenWait(dir))
		os.Exit(1)
	}
	os.Exit(m.Run())
}

// appendThenWait appends killAfter records one at a time, printing each seq
// once Append has returned, and then waits. It returns only on an error.
func appendThenWait(dir string) error {
	j, err := journal.Open(journal.Options{Dir: dir, MaxSegmentBytes: 4096})
	if err != nil {
		return err
	}
	for n := 1; n <= killAfter; n++ {
		seq, err := j.Append(context.Background(), replication(n))
		if err != nil {
			return err
		}
		fmt.Println(seq)
	}
	select {}
}

var recordedAt = time.Date(2026, 10, 15, 12, 0, 0, 0, time.UTC)

// replication is a record of about 200 bytes.
func replication(n int) journal.Record {
	payload, err := json.Marshal(edgejournal.Replication{
		SchemaVersion: edgejournal.SchemaVersion, Action: edgejournal.ActionReplicated, Group: "g1",
		Repository: "library/alpine", Tag: fmt.Sprintf("v%d", n), Digest: fmt.Sprintf("sha256:%064d", n),
	})
	if err != nil {
		panic(err)
	}
	return journal.Record{Type: edgejournal.RecordReplication, RecordedAt: recordedAt, Payload: payload}
}

func openJournal(t *testing.T, opts journal.Options) *journal.Journal {
	t.Helper()
	j, err := journal.Open(opts)
	require.NoError(t, err)
	t.Cleanup(func() {
		if err := j.Close(); err != nil && !errors.Is(err, journal.ErrClosed) {
			t.Errorf("close journal: %v", err)
		}
	})
	return j
}

func appendEach(t *testing.T, j *journal.Journal, n int) {
	t.Helper()
	for i := 1; i <= n; i++ {
		_, err := j.Append(context.Background(), replication(i))
		require.NoError(t, err)
	}
}

func readAll(t *testing.T, j *journal.Journal, afterSeq int64) []int64 {
	t.Helper()
	records, err := j.ReadFrom(afterSeq, 100000)
	require.NoError(t, err)
	return seqs(records)
}

func seqs(records []journal.Record) []int64 {
	out := make([]int64, 0, len(records))
	for _, r := range records {
		out = append(out, r.Seq)
	}
	return out
}

func seqRange(from, through int64) []int64 {
	out := make([]int64, 0, through-from+1)
	for s := from; s <= through; s++ {
		out = append(out, s)
	}
	return out
}

func segments(t *testing.T, dir string) []string {
	t.Helper()
	files, err := filepath.Glob(filepath.Join(dir, "seg-*.log"))
	require.NoError(t, err)
	return files
}

func segmentBytes(t *testing.T, dir string) int64 {
	t.Helper()
	var total int64
	for _, f := range segments(t, dir) {
		info, err := os.Stat(f)
		require.NoError(t, err)
		total += info.Size()
	}
	return total
}

func TestJournal_AppendAssignsMonotonicSeq(t *testing.T) {
	pending.Plan(t, "6.4", pendingJournal)
	ctx := context.Background()
	dir := t.TempDir()
	j := openJournal(t, journal.Options{Dir: dir})

	last, err := j.Append(ctx,
		journal.Record{Type: edgejournal.RecordCRI, RecordedAt: recordedAt, Payload: json.RawMessage(`{"n":1}`)},
		journal.Record{Type: edgejournal.RecordPullThrough, RecordedAt: recordedAt, Payload: json.RawMessage(`{"n":2}`)})
	require.NoError(t, err)
	require.EqualValues(t, 2, last)
	last, err = j.Append(ctx, replication(3))
	require.NoError(t, err)
	require.EqualValues(t, 3, last)

	got, err := j.ReadFrom(0, 10)
	require.NoError(t, err)
	require.Equal(t, []int64{1, 2, 3}, seqs(got))
	require.Equal(t, edgejournal.RecordPullThrough, got[1].Type)
	require.JSONEq(t, `{"n":2}`, string(got[1].Payload))
	require.True(t, got[1].RecordedAt.Equal(recordedAt))
	got, err = j.ReadFrom(1, 1)
	require.NoError(t, err)
	require.Equal(t, []int64{2}, seqs(got))

	require.NoError(t, j.Close())
	reopened := openJournal(t, journal.Options{Dir: dir})
	last, err = reopened.Append(ctx, replication(4))
	require.NoError(t, err)
	require.EqualValues(t, 4, last, "seqs continue after a restart")
}

func TestJournal_ConcurrentAppends(t *testing.T) {
	pending.Plan(t, "6.4", pendingJournal)
	j := openJournal(t, journal.Options{Dir: t.TempDir(), SyncInterval: 5 * time.Millisecond})
	const writers, each = 8, 25
	var wg sync.WaitGroup
	errs := make(chan error, writers*each)
	for w := range writers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := range each {
				if _, err := j.Append(context.Background(), replication(w*each+i)); err != nil {
					errs <- err
				}
			}
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		require.NoError(t, err)
	}

	require.Equal(t, seqRange(1, writers*each), readAll(t, j, 0), "every append got its own seq")
}

func TestJournal_EpochIsTheCreationTime(t *testing.T) {
	pending.Plan(t, "6.4", pendingJournal)
	dir := t.TempDir()
	before := time.Now().UnixMilli()
	first := openJournal(t, journal.Options{Dir: dir})
	epoch := first.Epoch()
	after := time.Now().UnixMilli()
	require.GreaterOrEqual(t, epoch, before)
	require.LessOrEqual(t, epoch, after)
	require.NoError(t, first.Close())

	require.Equal(t, epoch, openJournal(t, journal.Options{Dir: dir}).Epoch(), "a restart keeps the epoch")
	time.Sleep(2 * time.Millisecond)
	require.Greater(t, openJournal(t, journal.Options{Dir: t.TempDir()}).Epoch(), epoch, "a later journal has a later epoch")
}

// Ground Control's cursor holds a newer epoch of this satellite, as after
// a reinstall whose clock was behind: the journal continues above it.
func TestJournal_AckWithANewerEpochContinuesAboveIt(t *testing.T) {
	pending.Plan(t, "6.4", pendingJournal)
	j := openJournal(t, journal.Options{Dir: t.TempDir()})
	appendEach(t, j, 3)
	gcEpoch := j.Epoch() + 60_000

	require.NoError(t, j.Ack(gcEpoch, 0))

	require.Equal(t, gcEpoch+1, j.Epoch())
	require.Equal(t, []int64{1, 2, 3}, readAll(t, j, j.Floor()), "the records stay for the new epoch")
}

// An ack above the last seq means the directory was restored from a
// snapshot: the journal draws a newer epoch so its seqs are not taken for
// ones Ground Control already holds.
func TestJournal_AckAboveTheLastSeqStartsANewEpoch(t *testing.T) {
	pending.Plan(t, "6.4", pendingJournal)
	j := openJournal(t, journal.Options{Dir: t.TempDir()})
	appendEach(t, j, 3)
	epoch := j.Epoch()

	require.NoError(t, j.Ack(epoch, 500))

	require.Greater(t, j.Epoch(), epoch)
	require.Equal(t, []int64{1, 2, 3}, readAll(t, j, j.Floor()))
}

func TestJournal_TornTailIsTruncated(t *testing.T) {
	pending.Plan(t, "6.4", pendingJournal)
	dir := t.TempDir()
	j := openJournal(t, journal.Options{Dir: dir})
	appendEach(t, j, 3)
	require.NoError(t, j.Close())
	segs := segments(t, dir)
	require.NotEmpty(t, segs)
	f, err := os.OpenFile(segs[len(segs)-1], os.O_APPEND|os.O_WRONLY, 0)
	require.NoError(t, err)
	// A length prefix announcing more bytes than follow, as a crash
	// during a write leaves it.
	_, err = f.Write([]byte{0, 0, 1, 0, '{', '"', 's'})
	require.NoError(t, err)
	require.NoError(t, f.Close())

	reopened := openJournal(t, journal.Options{Dir: dir})
	require.Equal(t, []int64{1, 2, 3}, readAll(t, reopened, 0))
	last, err := reopened.Append(context.Background(), replication(4))
	require.NoError(t, err)
	require.EqualValues(t, 4, last)
	require.Equal(t, []int64{1, 2, 3, 4}, readAll(t, reopened, 0))
}

func TestJournal_AckDeletesSegments(t *testing.T) {
	pending.Plan(t, "6.4", pendingJournal)
	dir := t.TempDir()
	opts := journal.Options{Dir: dir, MaxSegmentBytes: 512}
	j := openJournal(t, opts)
	appendEach(t, j, 20)
	before := len(segments(t, dir))
	require.Greater(t, before, 2, "records of about 200 bytes fill several 512-byte segments")

	require.NoError(t, j.Ack(j.Epoch(), 15))
	require.EqualValues(t, 15, j.Acked())
	require.EqualValues(t, 15, j.Floor())
	require.Less(t, len(segments(t, dir)), before, "segments holding only acknowledged records are deleted")
	require.Equal(t, seqRange(16, 20), readAll(t, j, 15))
	require.NoError(t, j.Ack(j.Epoch(), 10))
	require.EqualValues(t, 15, j.Acked(), "an older ack does not move back")

	require.NoError(t, j.Close())
	reopened := openJournal(t, opts)
	require.EqualValues(t, 15, reopened.Acked(), "the ack survives a restart")
	require.NoError(t, reopened.Ack(reopened.Epoch(), 20))
	require.LessOrEqual(t, len(segments(t, dir)), 1, "only the segment being written may stay")
}

func TestJournal_TotalCapRaisesTheFloor(t *testing.T) {
	pending.Plan(t, "6.4", pendingJournal)
	dir := t.TempDir()
	opts := journal.Options{Dir: dir, MaxSegmentBytes: 512, MaxTotalBytes: 2048}
	j := openJournal(t, opts)
	appendEach(t, j, 100)

	require.LessOrEqual(t, segmentBytes(t, dir), opts.MaxTotalBytes+opts.MaxSegmentBytes)
	require.Zero(t, j.Acked(), "dropping is not an ack")
	floor := j.Floor()
	require.Positive(t, floor, "the oldest records were dropped")
	require.Equal(t, seqRange(floor+1, 100), readAll(t, j, floor), "everything above the floor is still there")
}

func TestJournal_TryAppendNeverBlocksAndCountsDrops(t *testing.T) {
	pending.Plan(t, "6.4", pendingJournal)
	j := openJournal(t, journal.Options{Dir: t.TempDir(), QueueSize: 2, SyncInterval: 200 * time.Millisecond})

	start := time.Now()
	var queued []bool
	for i := 1; i <= 10; i++ {
		queued = append(queued, j.TryAppend(replication(i)))
	}
	require.Less(t, time.Since(start), 10*time.Millisecond, "the producer never waits for a sync")
	require.Equal(t, []bool{true, true, false, false, false, false, false, false, false, false}, queued,
		"two slots until the batch is synced")
	var records []journal.Record
	require.Eventually(t, func() bool {
		var err error
		records, err = j.ReadFrom(0, 100)
		return err == nil && len(records) == 3
	}, 5*time.Second, 10*time.Millisecond)

	require.Equal(t, []int64{1, 2, 3}, seqs(records), "dropped records take no seqs")
	require.Equal(t, edgejournal.RecordDropped, records[2].Type)
	var dropped edgejournal.Dropped
	require.NoError(t, json.Unmarshal(records[2].Payload, &dropped))
	require.Equal(t, edgejournal.Dropped{SchemaVersion: edgejournal.SchemaVersion, Count: 8}, dropped)
	require.Zero(t, j.Floor())
}

// A130: an epoch move keeps the floor, or records the satellite gave up
// would be uploaded as if they still existed.
func TestJournal_FloorNeverDecreasesAcrossAnEpochMove(t *testing.T) {
	for name, ack := range map[string]func(j *journal.Journal) error{
		"newer Ground Control epoch": func(j *journal.Journal) error { return j.Ack(j.Epoch()+60_000, 500) },
		"ack above the last seq":     func(j *journal.Journal) error { return j.Ack(j.Epoch(), 500) },
	} {
		t.Run(name, func(t *testing.T) {
			pending.Plan(t, "6.4", pendingJournal)
			j := openJournal(t, journal.Options{Dir: t.TempDir()})
			appendEach(t, j, 300)
			require.NoError(t, j.Ack(j.Epoch(), 250))
			epoch := j.Epoch()

			require.NoError(t, ack(j))

			require.Greater(t, j.Epoch(), epoch)
			require.EqualValues(t, 250, j.Floor())
			require.Equal(t, seqRange(251, 300), readAll(t, j, j.Floor()))
		})
	}
}

func TestJournal_DropRaisesTheFloor(t *testing.T) {
	pending.Plan(t, "6.4", pendingJournal)
	j := openJournal(t, journal.Options{Dir: t.TempDir()})
	appendEach(t, j, 5)

	require.NoError(t, j.Drop(3))

	require.Zero(t, j.Acked())
	require.EqualValues(t, 3, j.Floor())
	require.Equal(t, []int64{4, 5}, readAll(t, j, j.Floor()))
}

func TestJournal_SurvivesKill9(t *testing.T) {
	pending.Plan(t, "6.4", pendingJournal)
	dir := t.TempDir()
	child := exec.Command(os.Args[0])
	child.Env = append(os.Environ(), killChildDir+"="+dir)
	var stderr bytes.Buffer
	child.Stderr = &stderr
	stdout, err := child.StdoutPipe()
	require.NoError(t, err)
	require.NoError(t, child.Start())
	exited := false
	done := make(chan struct{})
	t.Cleanup(func() {
		close(done)
		if exited {
			return
		}
		if err := child.Process.Kill(); err != nil {
			t.Logf("kill child: %v", err)
		}
		if err := child.Wait(); err != nil {
			t.Logf("child: %v", err)
		}
	})
	lines := make(chan string)
	go func() {
		defer close(lines)
		scanner := bufio.NewScanner(stdout)
		for scanner.Scan() {
			select {
			case lines <- scanner.Text():
			case <-done:
				return
			}
		}
	}()

	var printed []int64
	timeout := time.After(30 * time.Second)
	for len(printed) < killAfter {
		select {
		case line, ok := <-lines:
			require.True(t, ok, "the child exited after %d records: %s", len(printed), stderr.String())
			seq, err := strconv.ParseInt(strings.TrimSpace(line), 10, 64)
			require.NoError(t, err)
			printed = append(printed, seq)
		case <-timeout:
			t.Fatalf("the child appended only %d records in 30s: %s", len(printed), stderr.String())
		}
	}
	require.NoError(t, child.Process.Kill())
	var exitErr *exec.ExitError
	require.ErrorAs(t, child.Wait(), &exitErr)
	exited = true

	j := openJournal(t, journal.Options{Dir: dir, MaxSegmentBytes: 4096})
	records, err := j.ReadFrom(0, 100000)
	require.NoError(t, err)
	sort.Slice(printed, func(a, b int) bool { return printed[a] < printed[b] })
	require.Equal(t, printed, seqs(records), "every record whose Append returned is on disk")
	for _, r := range records {
		require.True(t, json.Valid(r.Payload), "record %d is corrupt", r.Seq)
	}
	next, err := j.Append(context.Background(), replication(0))
	require.NoError(t, err)
	require.EqualValues(t, len(records)+1, next)
}
