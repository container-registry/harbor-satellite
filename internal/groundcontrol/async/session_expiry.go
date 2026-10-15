package async

import (
	"context"
	"time"

	"github.com/container-registry/harbor-satellite/internal/groundcontrol/fleet"
	"github.com/container-registry/harbor-satellite/internal/shared/notimpl"
)

// ExpiredSessions is what SessionExpirer needs; postgres.SessionStore has it.
type ExpiredSessions interface {
	DeleteExpired(ctx context.Context, now time.Time) (int64, error)
}

// SessionExpirer deletes expired satellite sessions (plan item 4.1). The
// delete is idempotent, so every worker may run it.
type SessionExpirer struct{}

func NewSessionExpirer(store ExpiredSessions, clock fleet.Clock, interval time.Duration) (*SessionExpirer, error) {
	return nil, notimpl.Err("4.1 session expirer")
}

func (e *SessionExpirer) Name() string { return "session-expirer" }

// Run deletes the sessions expired at the clock's now every interval until
// ctx ends.
func (e *SessionExpirer) Run(ctx context.Context) error {
	return notimpl.Err("4.1 session expirer run")
}
