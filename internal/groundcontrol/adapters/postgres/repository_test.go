package postgres_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/container-registry/harbor-satellite/internal/groundcontrol/adapters/postgres"
	"github.com/container-registry/harbor-satellite/internal/groundcontrol/dbtest"
	"github.com/container-registry/harbor-satellite/internal/groundcontrol/fleet"
	"github.com/container-registry/harbor-satellite/internal/shared/testutil/pending"
	"github.com/stretchr/testify/require"
)

const pendingAdapter = "the postgres adapter is a stub; handlers call sqlc directly"

func newRepository(t *testing.T) *postgres.Repository {
	t.Helper()
	r, err := postgres.NewRepository(dbtest.New(t).DB)
	require.NoError(t, err)
	return r
}

func TestRepository_WithTxRollsBackOnError(t *testing.T) {
	pending.Plan(t, "1.3", pendingAdapter)
	repo := newRepository(t)
	ctx := context.Background()
	boom := errors.New("later step failed")

	err := repo.WithTx(ctx, func(tx fleet.Tx) error {
		if _, err := tx.Satellites().Create(ctx, "edge-01"); err != nil {
			return err
		}
		return boom
	})

	require.ErrorIs(t, err, boom)
	_, err = repo.Autocommit().Satellites().GetByName(ctx, "edge-01")
	require.ErrorIs(t, err, fleet.ErrNotFound)
}

func TestRepository_WithTxCommits(t *testing.T) {
	pending.Plan(t, "1.3", pendingAdapter)
	repo := newRepository(t)
	ctx := context.Background()

	require.NoError(t, repo.WithTx(ctx, func(tx fleet.Tx) error {
		_, err := tx.Satellites().Create(ctx, "edge-01")
		return err
	}))

	sat, err := repo.Autocommit().Satellites().GetByName(ctx, "edge-01")
	require.NoError(t, err)
	require.NotZero(t, sat.ID)
}

func TestRepository_RobotGrantsInOneStatement(t *testing.T) {
	pending.Plan(t, "1.3", pendingAdapter)
	db := dbtest.New(t)
	repo, err := postgres.NewRepository(db.DB)
	require.NoError(t, err)
	ctx := context.Background()
	tx := repo.Autocommit()
	groups := map[string]fleet.GroupID{}
	for name, projects := range map[string][]string{"g1": {"library"}, "g2": {"library", "team"}} {
		g, err := tx.Groups().Upsert(ctx, fleet.Group{Name: name, RegistryURL: "http://harbor.example", Projects: projects})
		require.NoError(t, err)
		groups[name] = g.ID
	}
	var ids []fleet.SatelliteID
	for i, member := range [][]string{{"g1", "g2"}, {"g1"}, {}} {
		sat, err := tx.Satellites().Create(ctx, fmt.Sprintf("edge-%02d", i))
		require.NoError(t, err)
		ids = append(ids, sat.ID)
		_, err = tx.Robots().Add(ctx, fleet.Robot{Satellite: sat.ID, Name: fmt.Sprintf("robot$edge-%02d", i), HarborID: int64(100 + i), SecretHash: "hash"})
		require.NoError(t, err)
		for _, g := range member {
			require.NoError(t, tx.Groups().AddMember(ctx, groups[g], sat.ID))
		}
	}

	var grants []fleet.RobotGrant
	stmts := db.MeasureStatements(t, func() {
		grants, err = tx.Satellites().RobotGrants(ctx, ids)
	})

	require.NoError(t, err)
	require.EqualValues(t, 1, stmts.Total, "%s", stmts)
	bySatellite := map[fleet.SatelliteID]fleet.RobotGrant{}
	for _, g := range grants {
		bySatellite[g.Satellite] = g
	}
	require.Len(t, bySatellite, 3)
	require.ElementsMatch(t, []string{"library", "team"}, distinct(bySatellite[ids[0]].Projects))
	require.ElementsMatch(t, []string{"library"}, distinct(bySatellite[ids[1]].Projects))
	require.Empty(t, bySatellite[ids[2]].Projects)
	require.Equal(t, int64(100), bySatellite[ids[0]].HarborID)
	require.Equal(t, "robot$edge-00", bySatellite[ids[0]].RobotName)
}

// A group's artifacts live in group_artifacts (A81): the API transaction
// writes them with the group, and the reconciler renders from them.
func TestRepository_GroupUpsertStoresArtifactsInGroupArtifacts(t *testing.T) {
	pending.Plan(t, "2.1", pendingAdapter)
	db := dbtest.New(t)
	repo, err := postgres.NewRepository(db.DB)
	require.NoError(t, err)
	ctx := context.Background()
	groups := repo.Autocommit().Groups()
	artifacts := []fleet.Artifact{{Repository: "library/alpine", Tags: []string{"3.20"}, Type: "image", Labels: map[string]any{"tier": "edge"}}}
	stored := func(id fleet.GroupID) string {
		t.Helper()
		var raw string
		require.NoError(t, db.QueryRowContext(ctx, "SELECT artifacts::TEXT FROM group_artifacts WHERE group_id = $1", id).Scan(&raw))
		return raw
	}

	g, err := groups.Upsert(ctx, fleet.Group{Name: "g1", RegistryURL: "http://harbor.example", Projects: []string{"library"}, Artifacts: artifacts})
	require.NoError(t, err)
	require.JSONEq(t, `[{"repository":"library/alpine","tag":["3.20"],"type":"image","labels":{"tier":"edge"}}]`, stored(g.ID))
	got, err := groups.GetByName(ctx, "g1")
	require.NoError(t, err)
	require.Equal(t, artifacts, got.Artifacts)

	again, err := groups.Upsert(ctx, fleet.Group{Name: "g1", RegistryURL: "http://harbor.example", Projects: []string{"team"}})
	require.NoError(t, err)

	require.Equal(t, g.ID, again.ID)
	require.JSONEq(t, `[]`, stored(g.ID), "no artifacts is an empty array")
	got, err = groups.GetByName(ctx, "g1")
	require.NoError(t, err)
	require.Equal(t, []string{"team"}, got.Projects)
	require.Empty(t, got.Artifacts)
}

func TestRepository_MapsDriverErrors(t *testing.T) {
	pending.Plan(t, "1.3", pendingAdapter)
	repo := newRepository(t)
	ctx := context.Background()
	tx := repo.Autocommit()
	_, err := tx.Satellites().Create(ctx, "edge-01")
	require.NoError(t, err)

	_, err = tx.Satellites().Create(ctx, "edge-01")
	require.ErrorIs(t, err, fleet.ErrAlreadyExists)
	_, err = tx.Satellites().GetByName(ctx, "missing")
	require.ErrorIs(t, err, fleet.ErrNotFound)
	_, err = tx.Configs().Get(ctx, 4711)
	require.ErrorIs(t, err, fleet.ErrNotFound)
	_, err = tx.Robots().BySatellite(ctx, 4711)
	require.ErrorIs(t, err, fleet.ErrNotFound)
}

func TestRepository_ConfigInUse(t *testing.T) {
	pending.Plan(t, "1.3", pendingAdapter)
	repo := newRepository(t)
	ctx := context.Background()
	tx := repo.Autocommit()
	cfg, err := tx.Configs().Create(ctx, fleet.Config{Name: "edge-config", RegistryURL: "http://harbor.example", Document: json.RawMessage(`{}`)})
	require.NoError(t, err)
	inUse, err := tx.Configs().InUse(ctx, cfg.ID)
	require.NoError(t, err)
	require.False(t, inUse)
	sat, err := tx.Satellites().Create(ctx, "edge-01")
	require.NoError(t, err)

	require.NoError(t, tx.Satellites().SetConfig(ctx, sat.ID, cfg.ID))

	inUse, err = tx.Configs().InUse(ctx, cfg.ID)
	require.NoError(t, err)
	require.True(t, inUse)
	got, err := tx.Satellites().Config(ctx, sat.ID)
	require.NoError(t, err)
	require.Equal(t, cfg.ID, got.ID)
}

func TestRepository_RobotRoundTripsHarborID(t *testing.T) {
	pending.Plan(t, "1.3", pendingAdapter)
	repo := newRepository(t)
	ctx := context.Background()
	tx := repo.Autocommit()
	sat, err := tx.Satellites().Create(ctx, "edge-01")
	require.NoError(t, err)
	expires := time.Date(2026, 11, 14, 12, 0, 0, 0, time.UTC)

	added, err := tx.Robots().Add(ctx, fleet.Robot{Satellite: sat.ID, Name: "robot$edge-01", HarborID: 1 << 40, SecretHash: "hash", ExpiresAt: expires})
	require.NoError(t, err)

	got, err := tx.Robots().BySatellite(ctx, sat.ID)
	require.NoError(t, err)
	require.Equal(t, added.ID, got.ID)
	require.Equal(t, int64(1<<40), got.HarborID)
	require.True(t, expires.Equal(got.ExpiresAt), "%v != %v", expires, got.ExpiresAt)
}

func TestRepository_TokensClaimOnceAndUnclaim(t *testing.T) {
	pending.Plan(t, "1.3", pendingAdapter)
	repo := newRepository(t)
	ctx := context.Background()
	tx := repo.Autocommit()
	sat, err := tx.Satellites().Create(ctx, "edge-01")
	require.NoError(t, err)
	now := time.Now()
	require.NoError(t, tx.Tokens().Add(ctx, sat.ID, "valid-token", now.Add(time.Hour)))
	require.NoError(t, tx.Tokens().Add(ctx, sat.ID, "expired-token", now.Add(-time.Hour)))

	id, err := tx.Tokens().Claim(ctx, "valid-token", now)
	require.NoError(t, err)
	require.Equal(t, sat.ID, id)
	_, err = tx.Tokens().Claim(ctx, "valid-token", now)
	require.ErrorIs(t, err, fleet.ErrTokenInvalid)
	require.ErrorContains(t, err, "already consumed")
	_, err = tx.Tokens().Claim(ctx, "unknown-token", now)
	require.ErrorIs(t, err, fleet.ErrTokenInvalid)
	require.NotContains(t, err.Error(), "already consumed")
	_, err = tx.Tokens().Claim(ctx, "expired-token", now)
	require.ErrorIs(t, err, fleet.ErrTokenExpired)

	require.NoError(t, tx.Tokens().Unclaim(ctx, "valid-token"))
	_, err = tx.Tokens().Claim(ctx, "valid-token", now)
	require.NoError(t, err)
	require.NoError(t, tx.Tokens().Consume(ctx, "valid-token"))
	_, err = tx.Tokens().Claim(ctx, "valid-token", now)
	require.ErrorIs(t, err, fleet.ErrTokenInvalid)
}

func distinct(xs []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, x := range xs {
		if !seen[x] {
			seen[x] = true
			out = append(out, x)
		}
	}
	return out
}
