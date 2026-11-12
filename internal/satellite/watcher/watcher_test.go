package watcher

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/require"
)

type watch struct {
	path   string
	events chan struct{}
	done   chan error
	cancel context.CancelFunc
}

func startWatch(t *testing.T, events chan struct{}) *watch {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	require.NoError(t, os.WriteFile(path, []byte(`{}`), 0o600))
	ctx, cancel := context.WithCancel(context.Background())
	w := &watch{path: path, events: events, done: make(chan error, 1), cancel: cancel}
	go func() { w.done <- WatchChanges(ctx, zerolog.Nop(), path, events) }()
	t.Cleanup(func() {
		cancel()
		<-w.done
	})
	// fsnotify registers the watch asynchronously to this goroutine; give
	// it a moment so the first write is not missed.
	time.Sleep(100 * time.Millisecond)
	return w
}

func requireEvent(t *testing.T, events <-chan struct{}, msg string) {
	t.Helper()
	select {
	case <-events:
	case <-time.After(3 * time.Second):
		t.Fatal(msg)
	}
}

func TestWriteNotifies(t *testing.T) {
	w := startWatch(t, make(chan struct{}, 1))
	require.NoError(t, os.WriteFile(w.path, []byte(`{"app_config":{}}`), 0o600))
	requireEvent(t, w.events, "no notification after writing the config file")
}

func TestCancelStopsWithoutError(t *testing.T) {
	events := make(chan struct{}, 1)
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	require.NoError(t, os.WriteFile(path, []byte(`{}`), 0o600))
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- WatchChanges(ctx, zerolog.Nop(), path, events) }()
	cancel()
	select {
	case err := <-done:
		require.NoError(t, err)
	case <-time.After(3 * time.Second):
		t.Fatal("WatchChanges did not return after cancel")
	}
}

func TestMissingFileIsAnError(t *testing.T) {
	err := WatchChanges(context.Background(), zerolog.Nop(), filepath.Join(t.TempDir(), "absent", "config.json"), make(chan struct{}))
	require.ErrorContains(t, err, "failed to add watch")
}

// A consumer still busy with the previous reload must not lose the write
// that follows: it has to see one more notification after the last write,
// or it keeps running the older config.
func TestWriteDuringABusyConsumerIsNotLost(t *testing.T) {
	w := startWatch(t, make(chan struct{}))
	require.NoError(t, os.WriteFile(w.path, []byte(`{"a":1}`), 0o600))
	time.Sleep(200 * time.Millisecond)
	require.NoError(t, os.WriteFile(w.path, []byte(`{"a":2}`), 0o600))
	time.Sleep(200 * time.Millisecond)

	requireEvent(t, w.events, "writes made while nobody was reading were dropped")
}
