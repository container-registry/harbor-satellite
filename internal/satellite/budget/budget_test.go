package budget

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestSnapshotCountsTheBodyAndTheLastSecond(t *testing.T) {
	account := New()
	body := account.wrapBody(context.Background(), io.NopCloser(bytes.NewReader([]byte("hello"))))
	got, err := io.ReadAll(body)
	require.NoError(t, err)
	require.Equal(t, "hello", string(got))
	require.NoError(t, body.Close())

	stats := account.Snapshot()
	require.Equal(t, int64(5), stats.BytesTotal)
	require.Equal(t, int64(5), stats.BytesInWindow)
	require.Equal(t, float64(5), stats.BytesPerSecond)
}

func TestBeginSyncResetsTheWindowAndKeepsTheTotal(t *testing.T) {
	account := New()
	account.record(80)
	account.BeginSync()
	account.record(4)

	stats := account.Snapshot()
	require.Equal(t, int64(84), stats.BytesTotal)
	require.Equal(t, int64(4), stats.BytesInWindow)
}

func TestOldSamplesLeaveTheRate(t *testing.T) {
	now := time.Date(2026, 11, 12, 12, 0, 0, 0, time.UTC)
	account := New()
	account.now = func() time.Time { return now }
	account.record(10)

	now = now.Add(2 * time.Second)
	account.record(3)

	stats := account.Snapshot()
	require.Equal(t, float64(3), stats.BytesPerSecond)
	require.Equal(t, int64(13), stats.BytesTotal)
}

func TestCanceledReadDoesNotCount(t *testing.T) {
	account := New()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	body := account.wrapBody(ctx, io.NopCloser(bytes.NewReader([]byte("hello"))))
	_, err := body.Read(make([]byte, 5))
	require.ErrorIs(t, err, context.Canceled)
	require.Equal(t, int64(0), account.Snapshot().BytesTotal)
	require.NoError(t, body.Close())
}

func TestContextTransportMetersOnlyMarkedRequests(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if _, err := w.Write([]byte("abcdef")); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
		}
	}))
	t.Cleanup(server.Close)

	client := &http.Client{Transport: ContextTransport(nil)}
	before := Ensure().Snapshot().BytesTotal

	plain, err := client.Get(server.URL)
	require.NoError(t, err)
	_, err = io.ReadAll(plain.Body)
	require.NoError(t, err)
	require.NoError(t, plain.Body.Close())
	require.Equal(t, before, Ensure().Snapshot().BytesTotal)

	request, err := http.NewRequest(http.MethodGet, server.URL, nil)
	require.NoError(t, err)
	request = request.WithContext(WithMeter(request.Context()))
	marked, err := client.Do(request)
	require.NoError(t, err)
	_, err = io.ReadAll(marked.Body)
	require.NoError(t, err)
	require.NoError(t, marked.Body.Close())
	require.Equal(t, before+6, Ensure().Snapshot().BytesTotal)
	require.Equal(t, before+6, Ensure().Snapshot().BytesInWindow)
}
