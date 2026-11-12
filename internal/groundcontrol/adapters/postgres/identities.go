package postgres

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"time"

	"github.com/container-registry/harbor-satellite/internal/groundcontrol/async"
	"github.com/container-registry/harbor-satellite/internal/groundcontrol/authn"
	"github.com/container-registry/harbor-satellite/internal/shared/notimpl"
)

var (
	_ authn.IdentityStore      = (*IdentityStore)(nil)
	_ async.ExpiredLoginStates = (*IdentityStore)(nil)
)

// IdentityStore runs UpsertOIDCUser, InsertOIDCLoginState,
// TakeOIDCLoginState and DeleteExpiredOIDCLoginStates (plan item 8.1).
// UpsertOIDCUser answering no row is retried once, because a concurrent
// first login of the same identity is invisible to the statement's
// snapshot; no row again means a local account holds the username:
// fleet.ErrForbidden.
type IdentityStore struct{}

func NewIdentityStore(db *sql.DB) (*IdentityStore, error) {
	return nil, notimpl.Err("8.1 postgres identity store")
}

func (s *IdentityStore) Upsert(ctx context.Context, claims authn.Claims, role string) (int32, error) {
	return 0, notimpl.Err("8.1 postgres upsert OIDC user")
}

func (s *IdentityStore) SaveLoginState(ctx context.Context, state authn.LoginState) error {
	return notimpl.Err("8.1 postgres save login state")
}

func (s *IdentityStore) TakeLoginState(ctx context.Context, state string, bindingHash [sha256.Size]byte, now time.Time) (string, string, error) {
	return "", "", notimpl.Err("8.1 postgres take login state")
}

func (s *IdentityStore) DeleteExpiredLoginStates(ctx context.Context, now time.Time) (int64, error) {
	return 0, notimpl.Err("8.1 postgres delete expired login states")
}
