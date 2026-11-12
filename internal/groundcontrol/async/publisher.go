package async

import (
	"context"
	"errors"
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/container-registry/harbor-satellite/internal/groundcontrol/fleet"
)

type PublisherConfig struct {
	BatchSize    int
	PollInterval time.Duration
	// Concurrency bounds the handlers running at once.
	Concurrency int
	// MaxAttempts is how often an event is claimed before it stays
	// failed; failed events show at /api/ops/outbox.
	MaxAttempts             int
	BackoffBase, BackoffMax time.Duration
	// Lease is how long a claimed event stays invisible to other workers;
	// a worker that dies mid-batch releases its events after it.
	Lease time.Duration
}

// DefaultPublisherConfig matches the OUTBOX_* defaults.
func DefaultPublisherConfig() PublisherConfig {
	return PublisherConfig{
		BatchSize:    100,
		PollInterval: time.Second,
		Concurrency:  4,
		MaxAttempts:  10,
		BackoffBase:  time.Second,
		BackoffMax:   5 * time.Minute,
		Lease:        time.Minute,
	}
}

type PublisherDeps struct {
	Store   OutboxStore
	Handler Handler
	// Locker is held around each event's handler. An event whose
	// aggregate another worker holds is released, not failed.
	Locker AggregateLocker
	// Registry pauses claiming while the registry is unreachable; when it
	// becomes reachable again the publisher re-arms the events that ran
	// out of attempts.
	Registry Reachability
}

// Publisher claims due outbox events and hands each to the handler under
// its aggregate's lock. A handled event is completed. A failure wrapping
// fleet.ErrRegistryUnavailable is released, so an outage of any length
// uses no attempts; ErrNoHandler is logged and completed; any other
// failure is failed with Backoff.
type Publisher struct {
	deps PublisherDeps
	cfg  PublisherConfig

	mu sync.Mutex
	// reachable is the registry's reachability at the last RunOnce. It
	// starts false, so the first reachable run re-arms what an outage
	// before a restart left dead.
	reachable bool
}

func NewPublisher(deps PublisherDeps, cfg PublisherConfig) (*Publisher, error) {
	if deps.Store == nil || deps.Handler == nil || deps.Locker == nil || deps.Registry == nil {
		return nil, fmt.Errorf("%w: the publisher needs a store, a handler, a locker and the registry's reachability", fleet.ErrInvalid)
	}
	def := DefaultPublisherConfig()
	if cfg.BatchSize <= 0 {
		cfg.BatchSize = def.BatchSize
	}
	if cfg.PollInterval <= 0 {
		cfg.PollInterval = def.PollInterval
	}
	if cfg.Concurrency <= 0 {
		cfg.Concurrency = def.Concurrency
	}
	if cfg.MaxAttempts <= 0 {
		cfg.MaxAttempts = def.MaxAttempts
	}
	if cfg.BackoffBase <= 0 {
		cfg.BackoffBase = def.BackoffBase
	}
	if cfg.BackoffMax < cfg.BackoffBase {
		cfg.BackoffMax = max(def.BackoffMax, cfg.BackoffBase)
	}
	if cfg.Lease <= 0 {
		cfg.Lease = def.Lease
	}
	return &Publisher{deps: deps, cfg: cfg}, nil
}

func (p *Publisher) Name() string { return "outbox-publisher" }

// Run polls until ctx ends and returns nil then. It retries its own
// failures (a lost database connection) instead of returning them.
func (p *Publisher) Run(ctx context.Context) error {
	for {
		select {
		case <-ctx.Done():
			return nil
		default:
		}
		n, err := p.RunOnce(ctx)
		if err != nil && ctx.Err() == nil {
			log.Printf("outbox publisher: %v", err)
		}
		if err == nil && n >= p.cfg.BatchSize {
			continue
		}
		timer := time.NewTimer(p.cfg.PollInterval)
		select {
		case <-ctx.Done():
			timer.Stop()
			return nil
		case <-timer.C:
		}
	}
}

// RunOnce claims one batch and handles it; it claims nothing while the
// registry is unreachable. Events handled before ctx ends are completed
// even if the batch is cut short.
func (p *Publisher) RunOnce(ctx context.Context) (processed int, err error) {
	if !p.checkReachable(ctx) {
		return 0, nil
	}
	events, err := p.deps.Store.Claim(ctx, p.cfg.BatchSize, p.cfg.MaxAttempts, p.cfg.Lease)
	if err != nil {
		return 0, fmt.Errorf("claim outbox events: %w", err)
	}
	if len(events) == 0 {
		return 0, nil
	}
	var (
		mu   sync.Mutex
		done []int64
		errs []error
		wg   sync.WaitGroup
	)
	slots := make(chan struct{}, p.cfg.Concurrency)
	for _, e := range events {
		if ctx.Err() != nil {
			break
		}
		slots <- struct{}{}
		wg.Go(func() {
			defer func() { <-slots }()
			completed, err := p.handle(ctx, e)
			mu.Lock()
			defer mu.Unlock()
			if completed {
				done = append(done, e.ID)
			}
			if err != nil {
				errs = append(errs, err)
			}
		})
	}
	wg.Wait()
	if len(done) > 0 {
		if err := p.deps.Store.Complete(context.WithoutCancel(ctx), done); err != nil {
			errs = append(errs, fmt.Errorf("complete outbox events: %w", err))
		}
	}
	if err := ctx.Err(); err != nil {
		errs = append(errs, err)
	}
	return len(done), errors.Join(errs...)
}

// checkReachable pauses claiming while the registry is unreachable and
// re-arms dead events when it comes back.
func (p *Publisher) checkReachable(ctx context.Context) bool {
	reachable := p.deps.Registry.Reachable()
	p.mu.Lock()
	cameBack := reachable && !p.reachable
	p.reachable = reachable
	p.mu.Unlock()
	if cameBack {
		if n, err := p.deps.Store.Rearm(ctx, p.cfg.MaxAttempts); err != nil {
			log.Printf("outbox publisher: re-arm dead events: %v", err)
		} else if n > 0 {
			log.Printf("outbox publisher: registry reachable again, re-armed %d events", n)
		}
	}
	return reachable
}

// handle runs one event under its aggregate's lock; completed is true when
// the event is done.
func (p *Publisher) handle(ctx context.Context, e OutboxEvent) (completed bool, err error) {
	store := p.deps.Store
	unlock, ok, err := p.deps.Locker.TryLock(ctx, e.Aggregate)
	if err != nil {
		return false, store.Release(context.WithoutCancel(ctx), e.ID, "aggregate lock: "+err.Error(), p.cfg.PollInterval)
	}
	if !ok {
		return false, store.Release(context.WithoutCancel(ctx), e.ID, "", p.cfg.PollInterval)
	}
	herr := p.deps.Handler.Handle(ctx, e)
	unlock()
	if herr != nil && ctx.Err() != nil {
		// Cut short: the lease brings the event back.
		return false, fmt.Errorf("event %d cut short: %w", e.ID, ctx.Err())
	}
	switch {
	case herr == nil:
		return true, nil
	case errors.Is(herr, ErrNoHandler):
		log.Printf("outbox publisher: event %d (%s): %v; completing it", e.ID, e.Kind, herr)
		return true, nil
	case errors.Is(herr, fleet.ErrRegistryUnavailable):
		return false, store.Release(context.WithoutCancel(ctx), e.ID, herr.Error(), p.cfg.BackoffBase)
	default:
		retryIn := Backoff(int(e.Attempts), p.cfg.BackoffBase, p.cfg.BackoffMax)
		return false, store.Fail(context.WithoutCancel(ctx), e.ID, herr.Error(), retryIn)
	}
}
