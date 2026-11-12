package heartbeat

import (
	"context"

	"github.com/container-registry/harbor-satellite/internal/shared/notimpl"
)

// Flusher writes the buffer to the store; every serve replica runs one,
// whatever --worker is.
type Flusher struct{}

func NewFlusher(buffer *Buffer, store Store, cfg Config) (*Flusher, error) {
	return nil, notimpl.Err("4.2 heartbeat flusher")
}

func (f *Flusher) Name() string { return "heartbeat-flusher" }

// Run flushes every FlushInterval and once more after ctx ends. After a
// failed batch it flushes satellite by satellite, so a poison row affects
// only its own satellite, which is dropped after MaxFlushFailures.
func (f *Flusher) Run(ctx context.Context) error {
	return notimpl.Err("4.2 heartbeat flusher run")
}

// Flush writes what is buffered now.
func (f *Flusher) Flush(ctx context.Context) error {
	return notimpl.Err("4.2 heartbeat flush")
}
