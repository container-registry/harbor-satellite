package events

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/container-registry/harbor-satellite/internal/satellite/scheduler"
	"github.com/container-registry/harbor-satellite/internal/shared/testutil/pending"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/require"
)

type countingProcess struct {
	name string
	runs atomic.Int32
}

func (p *countingProcess) Name() string { return p.name }

func (p *countingProcess) Execute(context.Context) error {
	p.runs.Add(1)
	return nil
}

func (p *countingProcess) IsRunning() bool  { return false }
func (p *countingProcess) IsComplete() bool { return false }

func TestEventScheduler_StopStopsRegisteredSchedulers(t *testing.T) {
	pending.Plan(t, "6.7", "event schedulers are never stopped")
	log := zerolog.Nop()
	proc := &countingProcess{name: "refresh_credentials"}
	sched, err := scheduler.NewScheduler(proc, &log)
	require.NoError(t, err)
	es := NewEventScheduler(&log)
	es.Register(context.Background(), sched)

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	require.NoError(t, es.Stop(ctx))

	runs := proc.runs.Load()
	sent := make(chan struct{})
	go func() {
		es.SendEvent(proc.name)
		close(sent)
	}()
	select {
	case <-sent:
	case <-time.After(time.Second):
		t.Fatal("SendEvent blocked after Stop")
	}
	time.Sleep(100 * time.Millisecond)
	require.Equal(t, runs, proc.runs.Load(), "an event after Stop does not run")
}

// SendEvent reads eventMap while Register writes it, without a lock.
func TestEventScheduler_SendEventTakesTheLock(t *testing.T) {
	pending.Plan(t, "6.7", "SendEvent reads the registrations without the lock Register holds")
	log := zerolog.Nop()
	es := NewEventScheduler(&log)
	es.mu.Lock()
	sent := make(chan struct{})
	go func() {
		es.SendEvent("refresh_credentials")
		close(sent)
	}()

	select {
	case <-sent:
		es.mu.Unlock()
		t.Fatal("SendEvent returned while Register held the lock")
	case <-time.After(50 * time.Millisecond):
	}
	es.mu.Unlock()
	select {
	case <-sent:
	case <-time.After(time.Second):
		t.Fatal("SendEvent did not return once the lock was free")
	}
}
