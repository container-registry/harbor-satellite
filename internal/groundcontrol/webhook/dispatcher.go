package webhook

import (
	"context"
	"time"

	"github.com/container-registry/harbor-satellite/internal/groundcontrol/fleet"
	"github.com/container-registry/harbor-satellite/internal/shared/notimpl"
)

type DispatcherConfig struct {
	BatchSize                                                    int
	PollInterval, BackoffBase, BackoffMax, Timeout, DisableAfter time.Duration
	Source                                                       string
}

// DefaultDispatcherConfig matches the WEBHOOK_* defaults.
func DefaultDispatcherConfig() DispatcherConfig {
	return DispatcherConfig{
		BatchSize:    100,
		PollInterval: time.Second,
		BackoffBase:  time.Second,
		BackoffMax:   5 * time.Minute,
		Timeout:      10 * time.Second,
		DisableAfter: 24 * time.Hour,
		Source:       "urn:harbor-satellite:ground-control",
	}
}

// Dispatcher delivers each subscription's events in (xid, id) order:
// filtered kinds advance the checkpoint without a send; a failure stops that
// subscription until its backoff, so nothing is skipped (at least once); a
// subscription failing for DisableAfter is disabled.
type Dispatcher struct{}

func NewDispatcher(subs SubscriptionStore, log LogReader, locker Locker, sealer fleet.Sealer, sender Sender, cfg DispatcherConfig) (*Dispatcher, error) {
	return nil, notimpl.Err("9.1 webhook dispatcher")
}

func (d *Dispatcher) Name() string { return "webhook-dispatcher" }

// Run calls RunOnce every PollInterval until ctx ends.
func (d *Dispatcher) Run(ctx context.Context) error {
	return notimpl.Err("9.1 webhook dispatcher run")
}

func (d *Dispatcher) RunOnce(ctx context.Context) (delivered int, err error) {
	return 0, notimpl.Err("9.1 webhook dispatcher run once")
}
