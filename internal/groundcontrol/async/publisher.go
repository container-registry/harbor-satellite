package async

import (
	"context"
	"time"

	"github.com/container-registry/harbor-satellite/internal/shared/notimpl"
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
type Publisher struct{}

func NewPublisher(deps PublisherDeps, cfg PublisherConfig) (*Publisher, error) {
	return nil, notimpl.Err("2.3 publisher")
}

func (p *Publisher) Name() string { return "outbox-publisher" }

// Run polls until ctx ends and returns nil then. It retries its own
// failures (a lost database connection) instead of returning them.
func (p *Publisher) Run(ctx context.Context) error {
	return notimpl.Err("2.3 publisher run")
}

// RunOnce claims one batch and handles it; it claims nothing while the
// registry is unreachable. Events handled before ctx ends are completed
// even if the batch is cut short.
func (p *Publisher) RunOnce(ctx context.Context) (processed int, err error) {
	return 0, notimpl.Err("2.3 publisher run once")
}
