//go:build !nospiffe

package spiffe

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"syscall"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func fakeEmbeddedSpire(t *testing.T, ready bool) (*EmbeddedSpireServer, string) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("fake SPIRE subprocess requires a POSIX shell")
	}
	dir := t.TempDir()
	server := NewEmbeddedSpireServer(&EmbeddedSpireConfig{
		DataDir:     filepath.Join(dir, "data"),
		TrustDomain: "example.com",
		BindAddress: "127.0.0.1",
		BindPort:    8081,
	})
	stopped := filepath.Join(dir, "stopped")
	t.Setenv("FAKE_SPIRE_SOCKET", server.socketPath)
	t.Setenv("FAKE_SPIRE_STOPPED", stopped)
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	script := "#!/bin/sh\ntrap 'printf stopped > \"$FAKE_SPIRE_STOPPED\"; exit 0' TERM\n"
	if ready {
		script += "touch \"$FAKE_SPIRE_SOCKET\"\n"
	}
	script += "while :; do sleep 0.05; done\n"
	require.NoError(t, os.WriteFile(filepath.Join(dir, "spire-server"), []byte(script), 0o700))
	t.Cleanup(func() {
		if server.cmd != nil && server.cmd.ProcessState == nil {
			require.NoError(t, server.Stop())
		}
	})
	return server, stopped
}

func TestEmbeddedSpireSurvivesStartupContextCancellation(t *testing.T) {
	server, stopped := fakeEmbeddedSpire(t, true)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	require.NoError(t, server.Start(ctx))

	cancel()
	// CommandContext would asynchronously kill the child as soon as ctx is canceled.
	time.Sleep(100 * time.Millisecond)
	require.NoError(t, server.cmd.Process.Signal(syscall.Signal(0)))
	require.NoError(t, server.Stop())
	require.FileExists(t, stopped, "Stop must let the subprocess handle SIGTERM")
}

func TestEmbeddedSpireCanceledStartupStopsProcess(t *testing.T) {
	server, stopped := fakeEmbeddedSpire(t, false)
	ctx, cancel := context.WithTimeout(context.Background(), 250*time.Millisecond)
	defer cancel()

	require.ErrorIs(t, server.Start(ctx), context.DeadlineExceeded)
	require.FileExists(t, stopped, "startup failure must gracefully stop the subprocess")
	require.True(t, errors.Is(server.cmd.Process.Signal(syscall.Signal(0)), os.ErrProcessDone))
}

func TestEmbeddedSpireCanceledContextDoesNotStartProcess(t *testing.T) {
	server, _ := fakeEmbeddedSpire(t, false)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	require.ErrorIs(t, server.Start(ctx), context.Canceled)
	require.Nil(t, server.cmd)
}
