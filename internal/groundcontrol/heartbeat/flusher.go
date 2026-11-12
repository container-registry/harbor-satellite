package heartbeat

import (
	"context"
	"errors"
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/container-registry/harbor-satellite/internal/groundcontrol/fleet"
)

// Flusher writes the buffer to the store; every serve replica runs one,
// whatever --worker is.
type Flusher struct {
	buffer *Buffer
	store  Store
	cfg    Config
	// mu serialises flushes: Run's ticker and a caller's Flush must not
	// drain into two concurrent transactions.
	mu       sync.Mutex
	failures map[fleet.SatelliteID]int
}

func NewFlusher(buffer *Buffer, store Store, cfg Config) (*Flusher, error) {
	if buffer == nil || store == nil {
		return nil, fmt.Errorf("%w: the heartbeat flusher needs a buffer and a store", fleet.ErrInvalid)
	}
	if err := cfg.validate(); err != nil {
		return nil, err
	}
	return &Flusher{buffer: buffer, store: store, cfg: cfg, failures: map[fleet.SatelliteID]int{}}, nil
}

func (f *Flusher) Name() string { return "heartbeat-flusher" }

// Run flushes every FlushInterval and once more after ctx ends. After a
// failed batch it flushes satellite by satellite, so a poison row affects
// only its own satellite, which is dropped after MaxFlushFailures.
func (f *Flusher) Run(ctx context.Context) error {
	ticker := time.NewTicker(f.cfg.FlushInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			final, cancel := context.WithTimeout(context.WithoutCancel(ctx), f.cfg.FinalFlushTimeout)
			defer cancel()
			if err := f.Flush(final); err != nil {
				log.Printf("heartbeat flusher: final flush: %v", err)
			}
			return nil
		case <-ticker.C:
			if err := f.Flush(ctx); err != nil && ctx.Err() == nil {
				log.Printf("heartbeat flusher: %v", err)
			}
		}
	}
}

// Flush writes what is buffered now.
func (f *Flusher) Flush(ctx context.Context) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	batch := f.buffer.Drain()
	if batch.empty() {
		return nil
	}
	err := f.store.Flush(ctx, batch)
	if err == nil {
		for _, beat := range batch.Latest {
			delete(f.failures, beat.Satellite)
		}
		return nil
	}
	if ctx.Err() != nil {
		f.buffer.Requeue(batch)
		return fmt.Errorf("flush heartbeats: %w", err)
	}
	var errs []error
	for _, part := range split(batch) {
		perr := f.store.Flush(ctx, part.batch)
		if perr == nil {
			delete(f.failures, part.satellite)
			continue
		}
		f.failures[part.satellite]++
		if n := f.failures[part.satellite]; n >= f.cfg.MaxFlushFailures {
			log.Printf("heartbeat flusher: dropping %d samples of satellite %d after %d failed flushes: %v", len(part.batch.Samples), part.satellite, n, perr)
		} else {
			f.buffer.Requeue(part.batch)
		}
		errs = append(errs, fmt.Errorf("satellite %d: %w", part.satellite, perr))
	}
	return errors.Join(errs...)
}
