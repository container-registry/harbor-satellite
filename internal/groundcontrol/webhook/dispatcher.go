package webhook

import (
	"context"
	"errors"
	"fmt"
	"log"
	"slices"
	"time"

	"github.com/container-registry/harbor-satellite/internal/groundcontrol/fleet"
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
type Dispatcher struct {
	subs   SubscriptionStore
	log    LogReader
	locker Locker
	sealer fleet.Sealer
	sender Sender
	cfg    DispatcherConfig
}

func NewDispatcher(subs SubscriptionStore, log LogReader, locker Locker, sealer fleet.Sealer, sender Sender, cfg DispatcherConfig) (*Dispatcher, error) {
	if subs == nil || log == nil || locker == nil || sealer == nil || sender == nil {
		return nil, fmt.Errorf("%w: the webhook dispatcher needs a subscription store, the outbox log, a locker, a sealer and a sender", fleet.ErrInvalid)
	}
	def := DefaultDispatcherConfig()
	if cfg.BatchSize <= 0 {
		cfg.BatchSize = def.BatchSize
	}
	if cfg.PollInterval <= 0 {
		cfg.PollInterval = def.PollInterval
	}
	if cfg.BackoffBase <= 0 {
		cfg.BackoffBase = def.BackoffBase
	}
	if cfg.BackoffMax <= 0 {
		cfg.BackoffMax = def.BackoffMax
	}
	if cfg.Timeout <= 0 {
		cfg.Timeout = def.Timeout
	}
	if cfg.DisableAfter <= 0 {
		cfg.DisableAfter = def.DisableAfter
	}
	if cfg.Source == "" {
		cfg.Source = def.Source
	}
	return &Dispatcher{subs: subs, log: log, locker: locker, sealer: sealer, sender: sender, cfg: cfg}, nil
}

func (d *Dispatcher) Name() string { return "webhook-dispatcher" }

// Run calls RunOnce every PollInterval until ctx ends.
func (d *Dispatcher) Run(ctx context.Context) error {
	ticker := time.NewTicker(d.cfg.PollInterval)
	defer ticker.Stop()
	for {
		if _, err := d.RunOnce(ctx); err != nil && ctx.Err() == nil {
			log.Printf("webhook dispatcher: %v", err)
		}
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
		}
	}
}

func (d *Dispatcher) RunOnce(ctx context.Context) (delivered int, err error) {
	due, err := d.subs.Due(ctx, d.cfg.BatchSize)
	if err != nil {
		return 0, fmt.Errorf("due webhook subscriptions: %w", err)
	}
	var errs []error
	for _, sub := range due {
		n, err := d.deliver(ctx, sub)
		delivered += n
		if err != nil {
			errs = append(errs, fmt.Errorf("subscription %d: %w", sub.ID, err))
		}
	}
	return delivered, errors.Join(errs...)
}

// deliver sends one subscription's events after its checkpoint until the
// first failure, under the subscription's lock. The checkpoint is reread
// under the lock, so events another worker delivered meanwhile are not
// sent again.
func (d *Dispatcher) deliver(ctx context.Context, listed DueSubscription) (int, error) {
	unlock, ok, err := d.locker.TryLock(ctx, listed.ID)
	if err != nil || !ok {
		return 0, err
	}
	defer unlock()
	sub, ok, err := d.subs.Refresh(ctx, listed.ID)
	if err != nil {
		return 0, fmt.Errorf("reread checkpoint: %w", err)
	}
	if !ok {
		return 0, nil
	}
	secret, err := d.sealer.Open(sub.SealedSecret)
	if err != nil {
		return 0, fmt.Errorf("open secret: %w", err)
	}
	entries, err := d.log.ReadAfter(ctx, sub.After, d.cfg.BatchSize)
	if err != nil {
		return 0, fmt.Errorf("read the outbox log: %w", err)
	}
	var (
		delivered, lastStatus int
		last                  = sub.After
		failure               error
		failStatus            int
	)
	for _, e := range entries {
		if len(sub.EventTypes) > 0 && !slices.Contains(sub.EventTypes, e.Kind) {
			last = e.Position
			continue
		}
		event, err := NewCloudEvent(e, d.cfg.Source)
		if err != nil {
			failure = err
			break
		}
		sendCtx, cancel := context.WithTimeout(ctx, d.cfg.Timeout)
		status, err := d.sender.Send(sendCtx, sub.URL, secret, event)
		cancel()
		if err != nil || status < 200 || status > 299 {
			failStatus = status
			failure = err
			if failure == nil {
				failure = fmt.Errorf("receiver answered %d", status)
			}
			break
		}
		delivered++
		last, lastStatus = e.Position, status
	}
	var errs []error
	if last != sub.After {
		if err := d.subs.Advance(ctx, sub.ID, last, lastStatus); err != nil {
			errs = append(errs, fmt.Errorf("advance checkpoint: %w", err))
		}
	}
	if failure != nil {
		disabled, err := d.subs.Fail(ctx, sub.ID, failStatus, failure.Error(), d.backoff(sub.Attempts), d.cfg.DisableAfter)
		if err != nil {
			errs = append(errs, fmt.Errorf("record failure: %w", err))
		}
		if disabled {
			log.Printf("webhook subscription %d disabled after failing for %s: %v", sub.ID, d.cfg.DisableAfter, failure)
		}
	}
	return delivered, errors.Join(errs...)
}

// backoff doubles BackoffBase per failed attempt up to BackoffMax.
func (d *Dispatcher) backoff(attempts int32) time.Duration {
	wait := d.cfg.BackoffBase
	for i := int32(0); i < attempts && wait < d.cfg.BackoffMax; i++ {
		wait *= 2
	}
	return min(wait, d.cfg.BackoffMax)
}
