package postgres_test

import (
	"context"
	"testing"

	"github.com/container-registry/harbor-satellite/internal/groundcontrol/adapters/postgres"
	"github.com/container-registry/harbor-satellite/internal/groundcontrol/authn"
	"github.com/container-registry/harbor-satellite/internal/groundcontrol/database"
	"github.com/container-registry/harbor-satellite/internal/groundcontrol/dbtest"
	"github.com/container-registry/harbor-satellite/internal/groundcontrol/fleet"
	"github.com/container-registry/harbor-satellite/internal/shared/testutil/pending"
	"github.com/stretchr/testify/require"
)

func TestIdentityStore_NeverAdoptsLocalAccount(t *testing.T) {
	pending.Plan(t, "8.1", "the postgres identity store is a stub; there is no OIDC login")
	db := dbtest.New(t)
	ctx := context.Background()
	_, err := database.New(db.DB).CreateUser(ctx, database.CreateUserParams{Username: "bob", PasswordHash: "local-hash", Role: "admin"})
	require.NoError(t, err)
	store, err := postgres.NewIdentityStore(db.DB)
	require.NoError(t, err)

	_, err = store.Upsert(ctx, authn.Claims{Issuer: "https://dex.example", Subject: "sub-bob", Username: "bob"}, "system_admin")
	require.ErrorIs(t, err, fleet.ErrForbidden)

	alice := authn.Claims{Issuer: "https://dex.example", Subject: "sub-alice", Username: "alice", Groups: []string{"edge-ops"}}
	id, err := store.Upsert(ctx, alice, "admin")
	require.NoError(t, err)
	again, err := store.Upsert(ctx, alice, "admin")
	require.NoError(t, err)
	require.Equal(t, id, again)
}
