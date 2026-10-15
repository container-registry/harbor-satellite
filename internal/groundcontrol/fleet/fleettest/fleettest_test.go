package fleettest_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/container-registry/harbor-satellite/internal/groundcontrol/fleet"
	"github.com/container-registry/harbor-satellite/internal/groundcontrol/fleet/fleettest"
	"github.com/stretchr/testify/require"
)

func TestFakesShareOneSequence(t *testing.T) {
	rec := fleettest.NewRecorder()
	repo := fleettest.NewRepository(rec)
	registry := fleettest.NewRegistry(rec)
	boom := errors.New("registry down")
	registry.FailOn("CreateRobot", boom)

	err := repo.WithTx(context.Background(), func(tx fleet.Tx) error {
		if _, err := tx.Satellites().Create(context.Background(), "edge-01"); err != nil {
			return err
		}
		_, err := registry.CreateRobot(context.Background(), fleet.RobotSpec{Name: "edge-01"})
		return err
	})

	require.ErrorIs(t, err, boom)
	require.Equal(t, []string{"WithTx", "Satellites.Create", "CreateRobot", "Rollback"}, rec.Methods())
	require.Equal(t, []any{"edge-01"}, rec.CallsTo("Satellites.Create")[0].Args)
}

func TestRepository_CommitsAndFailsCommit(t *testing.T) {
	repo := fleettest.NewRepository(nil)
	require.NoError(t, repo.WithTx(context.Background(), func(fleet.Tx) error { return nil }))
	require.Equal(t, []string{"WithTx", "Commit"}, repo.Methods())

	boom := errors.New("commit failed")
	repo.FailOn("Commit", boom)
	require.ErrorIs(t, repo.WithTx(context.Background(), func(fleet.Tx) error { return nil }), boom)
	repo.ClearFaults()
	require.NoError(t, repo.WithTx(context.Background(), func(fleet.Tx) error { return nil }))
}

func TestRepository_LookupsAnswerFromSeeds(t *testing.T) {
	ctx := context.Background()
	repo := fleettest.NewRepository(nil)
	repo.Satellites.ByName = map[string]fleet.Satellite{"edge-01": {ID: 7, Name: "edge-01"}}
	repo.Groups.ByName = map[string]fleet.Group{"g1": {ID: 1, Name: "g1"}}
	repo.Groups.NextID = 9
	repo.Tokens.Claimable = map[string]fleet.SatelliteID{"tok": 7}
	tx := repo.Autocommit()

	sat, err := tx.Satellites().Get(ctx, 7)
	require.NoError(t, err)
	require.Equal(t, "edge-01", sat.Name)
	_, err = tx.Satellites().GetByName(ctx, "edge-02")
	require.ErrorIs(t, err, fleet.ErrNotFound)
	existing, err := tx.Groups().Upsert(ctx, fleet.Group{Name: "g1"})
	require.NoError(t, err)
	require.Equal(t, fleet.GroupID(1), existing.ID)
	created, err := tx.Groups().Upsert(ctx, fleet.Group{Name: "g2"})
	require.NoError(t, err)
	require.Equal(t, fleet.GroupID(9), created.ID)
	id, err := tx.Tokens().Claim(ctx, "tok", time.Now())
	require.NoError(t, err)
	require.Equal(t, fleet.SatelliteID(7), id)
	_, err = tx.Tokens().Claim(ctx, "other", time.Now())
	require.ErrorIs(t, err, fleet.ErrTokenInvalid)
	event := fleet.Event{Kind: fleet.EventGroupChanged, Aggregate: fleet.Aggregate{Type: fleet.AggregateGroup, ID: 1}}
	require.ErrorIs(t, tx.Events().Append(ctx, event), fleet.ErrOutsideTransaction)
	require.NoError(t, repo.WithTx(ctx, func(tx fleet.Tx) error { return tx.Events().Append(ctx, event) }))
	require.Equal(t, []fleet.Event{event}, repo.Events.Appended())
}
