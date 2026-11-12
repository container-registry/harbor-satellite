package async

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/container-registry/harbor-satellite/internal/groundcontrol/fleet"
)

// ExpiredSessions is what SessionExpirer needs; postgres.SessionStore has it.
type ExpiredSessions interface {
	DeleteExpired(ctx context.Context, now time.Time) (int64, error)
}

// SessionExpirer deletes expired satellite sessions (plan item 4.1). The
// delete is idempotent, so every worker may run it.
type SessionExpirer struct {
	store    ExpiredSessions
	clock    fleet.Clock
	interval time.Duration
}

var _ Job = (*SessionExpirer)(nil)

func NewSessionExpirer(store ExpiredSessions, clock fleet.Clock, interval time.Duration) (*SessionExpirer, error) {
	if store == nil || clock == nil || interval <= 0 {
		return nil, fmt.Errorf("%w: the session expirer needs a store, a clock and a positive interval", fleet.ErrInvalid)
	}
	return &SessionExpirer{store: store, clock: clock, interval: interval}, nil
}

func (e *SessionExpirer) Name() string { return "session-expirer" }

// Run deletes the sessions expired at the clock's now every interval until
// ctx ends.
func (e *SessionExpirer) Run(ctx context.Context) error {
	every(ctx, e.interval, func(ctx context.Context) {
		if _, err := e.store.DeleteExpired(ctx, e.clock.Now()); err != nil && ctx.Err() == nil {
			log.Printf("session expirer: %v", err)
		}
	})
	return nil
}
