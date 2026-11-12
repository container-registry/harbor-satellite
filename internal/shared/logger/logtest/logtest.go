// Package logtest captures the satellite's context-carried zerolog output in
// tests, for example to assert that a secret never reaches a log.
package logtest

import (
	"bytes"
	"context"
	"sync"
	"testing"

	"github.com/container-registry/harbor-satellite/internal/shared/logger"
	"github.com/rs/zerolog"
)

// Buffer is a log sink that is safe for concurrent writers.
type Buffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *Buffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *Buffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

// Capture returns a context whose logger.FromContext writes JSON at every
// level to the returned buffer, plus that logger for code that takes it as
// an argument.
func Capture(t testing.TB) (context.Context, *zerolog.Logger, *Buffer) {
	t.Helper()
	buf := &Buffer{}
	log := zerolog.New(buf).Level(zerolog.TraceLevel)
	return context.WithValue(t.Context(), logger.LoggerKey, &log), &log, buf
}
