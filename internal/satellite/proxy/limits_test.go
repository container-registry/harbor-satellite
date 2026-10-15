package proxy_test

import (
	"bytes"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/container-registry/harbor-satellite/internal/satellite/proxy"
	"github.com/container-registry/harbor-satellite/internal/shared/testutil/pending"
	"github.com/stretchr/testify/require"
)

func TestWithWriteDeadline_ExtendsPerWrite(t *testing.T) {
	pending.Plan(t, "5.4", "the proxy server sets no write deadline, so a stalled client holds its connection forever")
	const perWrite = 300 * time.Millisecond
	chunk := bytes.Repeat([]byte("x"), 64<<10)
	writeFailed := make(chan error, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w = proxy.WithWriteDeadline(w, perWrite)
		if r.URL.Path == "/steady" {
			for range 8 {
				if _, err := w.Write(chunk); err != nil {
					return
				}
				time.Sleep(perWrite / 2)
			}
			return
		}
		for {
			if _, err := w.Write(chunk); err != nil {
				writeFailed <- err
				return
			}
		}
	}))
	t.Cleanup(server.Close)

	start := time.Now()
	resp, err := http.Get(server.URL + "/steady")
	require.NoError(t, err)
	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	require.NoError(t, resp.Body.Close())
	require.Len(t, body, 8*len(chunk), "a client that keeps reading gets the whole body")
	require.Greater(t, time.Since(start), perWrite)

	conn, err := net.Dial("tcp", server.Listener.Addr().String())
	require.NoError(t, err)
	t.Cleanup(func() { _ = conn.Close() })
	_, err = conn.Write([]byte("GET /stalled HTTP/1.1\r\nHost: satellite\r\n\r\n"))
	require.NoError(t, err)
	select {
	case err := <-writeFailed:
		require.Error(t, err)
	case <-time.After(5 * time.Second):
		require.NoError(t, conn.Close())
		t.Fatal("a write to a client that stopped reading never timed out")
	}
}
