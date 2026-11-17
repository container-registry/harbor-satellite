package async

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/container-registry/harbor-satellite/internal/groundcontrol/fleet"
)

// ExpiredLoginStates is what LoginStateExpirer needs; postgres.IdentityStore
// has it.
type ExpiredLoginStates interface {
	DeleteExpiredLoginStates(ctx context.Context, now time.Time) (int64, error)
}

// LoginStateExpirer deletes expired OIDC login states (plan item 8.1):
// /oidc/login stores one per anonymous request and only a matching
// callback takes it. The delete is idempotent, so every worker may run it.
type LoginStateExpirer struct {
	store    ExpiredLoginStates
	clock    fleet.Clock
	interval time.Duration
}

var _ Job = (*LoginStateExpirer)(nil)

func NewLoginStateExpirer(store ExpiredLoginStates, clock fleet.Clock, interval time.Duration) (*LoginStateExpirer, error) {
	if store == nil || clock == nil || interval <= 0 {
		return nil, fmt.Errorf("%w: the login state expirer needs a store, a clock and a positive interval", fleet.ErrInvalid)
	}
	return &LoginStateExpirer{store: store, clock: clock, interval: interval}, nil
}

func (e *LoginStateExpirer) Name() string { return "login-state-expirer" }

// Run deletes the states expired at the clock's now every interval until
// ctx ends.
func (e *LoginStateExpirer) Run(ctx context.Context) error {
	every(ctx, e.interval, func(ctx context.Context) {
		if _, err := e.store.DeleteExpiredLoginStates(ctx, e.clock.Now()); err != nil && ctx.Err() == nil {
			log.Printf("login state expirer: %v", err)
		}
	})
	return nil
}
