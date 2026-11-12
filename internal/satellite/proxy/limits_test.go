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
	"github.com/stretchr/testify/require"
)

func TestWithWriteDeadline_ExtendsPerWrite(t *testing.T) {
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

func TestWriteDeadlineHandler_KeptAliveConnectionOutlivesDeadline(t *testing.T) {
	const perWrite = 100 * time.Millisecond
	server := httptest.NewServer(proxy.WriteDeadlineHandler(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodHead {
			w.WriteHeader(http.StatusOK)
			return
		}
		_, _ = w.Write([]byte("ok"))
	}), perWrite))
	t.Cleanup(server.Close)
	client := server.Client()

	for i, method := range []string{http.MethodGet, http.MethodHead, http.MethodGet} {
		if i > 0 {
			time.Sleep(3 * perWrite)
		}
		req, err := http.NewRequestWithContext(t.Context(), method, server.URL, nil)
		require.NoError(t, err)
		resp, err := client.Do(req)
		require.NoError(t, err, "request %d on the kept-alive connection", i)
		_, err = io.Copy(io.Discard, resp.Body)
		require.NoError(t, err)
		require.NoError(t, resp.Body.Close())
		require.Equal(t, http.StatusOK, resp.StatusCode)
	}
}

func TestLimitListener_CapsConnections(t *testing.T) {
	inner, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	limited := proxy.LimitListener(inner, 1)
	t.Cleanup(func() { _ = limited.Close() })
	require.Same(t, inner, proxy.LimitListener(inner, 0))

	accepted := make(chan net.Conn, 2)
	go func() {
		for {
			conn, err := limited.Accept()
			if err != nil {
				return
			}
			accepted <- conn
		}
	}()
	first, err := net.Dial("tcp", inner.Addr().String())
	require.NoError(t, err)
	t.Cleanup(func() { _ = first.Close() })
	firstServer := <-accepted

	second, err := net.Dial("tcp", inner.Addr().String())
	require.NoError(t, err)
	t.Cleanup(func() { _ = second.Close() })
	select {
	case <-accepted:
		t.Fatal("a second connection was accepted while the first was open")
	case <-time.After(200 * time.Millisecond):
	}
	require.NoError(t, firstServer.Close())
	select {
	case conn := <-accepted:
		require.NoError(t, conn.Close())
	case <-time.After(5 * time.Second):
		t.Fatal("the second connection was not accepted after the first closed")
	}
}
