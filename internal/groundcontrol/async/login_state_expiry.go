package async

import (
	"context"
	"time"

	"github.com/container-registry/harbor-satellite/internal/groundcontrol/fleet"
	"github.com/container-registry/harbor-satellite/internal/shared/notimpl"
)

// ExpiredLoginStates is what LoginStateExpirer needs; postgres.IdentityStore
// has it.
type ExpiredLoginStates interface {
	DeleteExpiredLoginStates(ctx context.Context, now time.Time) (int64, error)
}

// LoginStateExpirer deletes expired OIDC login states (plan item 8.1):
// /oidc/login stores one per anonymous request and only a matching
// callback takes it. The delete is idempotent, so every worker may run it.
type LoginStateExpirer struct{}

var _ Job = (*LoginStateExpirer)(nil)

func NewLoginStateExpirer(store ExpiredLoginStates, clock fleet.Clock, interval time.Duration) (*LoginStateExpirer, error) {
	return nil, notimpl.Err("8.1 login state expirer")
}

func (e *LoginStateExpirer) Name() string { return "login-state-expirer" }

// Run deletes the states expired at the clock's now every interval until
// ctx ends.
func (e *LoginStateExpirer) Run(ctx context.Context) error {
	return notimpl.Err("8.1 login state expirer run")
}
