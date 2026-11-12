package budget

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/container-registry/harbor-satellite/pkg/config"
	"github.com/stretchr/testify/require"
)

func TestDisabledPolicyStillReportsThroughput(t *testing.T) {
	account := New()
	body := account.Wrap(io.NopCloser(bytes.NewReader([]byte("hello"))))
	got, err := io.ReadAll(body)
	require.NoError(t, err)
	require.Equal(t, "hello", string(got))
	require.NoError(t, body.Close())
	stats := account.Snapshot()
	require.Equal(t, int64(5), stats.BytesTotal)
	require.Equal(t, float64(5), stats.BytesPerSecond)
}

func TestWrapCountsBytesAndStopsWhenCanceled(t *testing.T) {
	account := New()
	account.SetPolicy(Policy{Speed: Speed{Enabled: true, BytesPerSecond: 1}})
	payload := bytes.Repeat([]byte("x"), 8)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	body := account.WrapContext(ctx, io.NopCloser(bytes.NewReader(payload)))

	buf := make([]byte, 4)
	n, err := body.Read(buf)
	require.NoError(t, err)
	require.Equal(t, 1, n)

	cancel()
	_, err = body.Read(buf)
	require.ErrorContains(t, err, "context canceled")
	require.NoError(t, body.Close())
	require.Equal(t, int64(1), account.Snapshot().BytesTotal)
}

type shortReader struct {
	r io.Reader
}

func (s shortReader) Read(p []byte) (int, error) {
	if len(p) == 0 {
		return 0, nil
	}
	return s.r.Read(p[:1])
}

func TestShortReadDoesNotSpendTheWholeBurst(t *testing.T) {
	account := New()
	account.SetPolicy(Policy{Speed: Speed{Enabled: true, BytesPerSecond: 1000}})
	payload := bytes.Repeat([]byte("x"), 4)
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	body := account.WrapContext(ctx, io.NopCloser(shortReader{r: bytes.NewReader(payload)}))

	buf := make([]byte, 1000)
	for range 3 {
		n, err := body.Read(buf)
		require.NoError(t, err)
		require.Equal(t, 1, n)
	}
	require.NoError(t, body.Close())
	require.Equal(t, int64(3), account.Snapshot().BytesTotal)
}

func TestPolicyFromDiagramQuantities(t *testing.T) {
	policy, err := PolicyFrom(config.NetworkConfig{
		Speed: config.NetworkSpeedConfig{Enabled: true, Limit: "10Mbps"},
	})
	require.NoError(t, err)
	require.Equal(t, int64(1_250_000), policy.Speed.BytesPerSecond)

	policy, err = PolicyFrom(config.NetworkConfig{
		Speed: config.NetworkSpeedConfig{Enabled: false, Limit: "10Mbps"},
	})
	require.NoError(t, err)
	require.False(t, policy.Speed.Enabled)
}

func TestContextTransportMetersOnlyMarkedRequests(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if _, err := w.Write([]byte("abcdef")); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
		}
	}))
	t.Cleanup(server.Close)
	require.NoError(t, Apply(config.NetworkConfig{
		Speed: config.NetworkSpeedConfig{Enabled: true, Limit: "10Mbps"},
	}))
	t.Cleanup(func() {
		require.NoError(t, Apply(config.NetworkConfig{}))
	})

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
}

func TestSetPolicyUpdatesTheLiveLimiter(t *testing.T) {
	account := New()
	account.SetPolicy(Policy{Speed: Speed{Enabled: true, BytesPerSecond: 1}})
	account.SetPolicy(Policy{Speed: Speed{Enabled: true, BytesPerSecond: 2_000}})
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	body := account.WrapContext(ctx, io.NopCloser(bytes.NewReader(bytes.Repeat([]byte("x"), 32))))
	got := make([]byte, 32)
	n, err := io.ReadFull(body, got)
	require.NoError(t, err)
	require.Equal(t, 32, n)
	require.NoError(t, body.Close())
	require.Equal(t, int64(32), account.Snapshot().BytesTotal)
}
