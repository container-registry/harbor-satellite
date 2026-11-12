package postgres

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/container-registry/harbor-satellite/internal/groundcontrol/async"
	"github.com/container-registry/harbor-satellite/internal/groundcontrol/authn"
	"github.com/container-registry/harbor-satellite/internal/groundcontrol/database"
	"github.com/container-registry/harbor-satellite/internal/groundcontrol/fleet"
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
type IdentityStore struct{ q *database.Queries }

func NewIdentityStore(db *sql.DB) (*IdentityStore, error) {
	if db == nil {
		return nil, fmt.Errorf("%w: the identity store needs a database", fleet.ErrInvalid)
	}
	return &IdentityStore{q: database.New(db)}, nil
}

func (s *IdentityStore) Upsert(ctx context.Context, claims authn.Claims, role string) (int32, error) {
	groups := claims.Groups
	if groups == nil {
		groups = []string{}
	}
	params := database.UpsertOIDCUserParams{
		Groups: groups, Email: sql.NullString{String: claims.Email, Valid: claims.Email != ""}, Now: time.Now(),
		Role: role, Issuer: claims.Issuer, Subject: claims.Subject, Username: claims.Username,
	}
	for range 2 {
		id, err := s.q.UpsertOIDCUser(ctx, params)
		if err == nil {
			return id, nil
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return 0, mapError(err)
		}
	}
	return 0, fmt.Errorf("%w: the username %q belongs to a local account", fleet.ErrForbidden, claims.Username)
}

func (s *IdentityStore) SaveLoginState(ctx context.Context, state authn.LoginState) error {
	return mapError(s.q.InsertOIDCLoginState(ctx, database.InsertOIDCLoginStateParams{
		State: state.State, CodeVerifier: state.CodeVerifier, Nonce: state.Nonce,
		BindingHash: state.BindingHash[:], ExpiresAt: state.ExpiresAt,
	}))
}

func (s *IdentityStore) TakeLoginState(ctx context.Context, state string, bindingHash [sha256.Size]byte, now time.Time) (string, string, error) {
	row, err := s.q.TakeOIDCLoginState(ctx, database.TakeOIDCLoginStateParams{State: state, BindingHash: bindingHash[:], Now: now})
	if errors.Is(err, sql.ErrNoRows) {
		return "", "", fmt.Errorf("%w: unknown, expired or used login state, or another browser", fleet.ErrForbidden)
	}
	if err != nil {
		return "", "", mapError(err)
	}
	return row.CodeVerifier, row.Nonce, nil
}

func (s *IdentityStore) DeleteExpiredLoginStates(ctx context.Context, now time.Time) (int64, error) {
	n, err := s.q.DeleteExpiredOIDCLoginStates(ctx, now)
	return n, mapError(err)
}
