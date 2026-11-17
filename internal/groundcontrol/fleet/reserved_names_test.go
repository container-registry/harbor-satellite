package fleet_test

import (
	"context"
	"testing"

	"github.com/container-registry/harbor-satellite/internal/groundcontrol/fleet"
	"github.com/stretchr/testify/require"
)

func TestReservedNames(t *testing.T) {
	ctx := context.Background()
	for _, name := range append(append([]string{}, fleet.ReservedSatelliteNames...), fleet.ReservedGroupNames...) {
		require.True(t, fleet.ValidName(name), "state refs of existing resources named %q keep working", name)
	}

	for _, name := range fleet.ReservedSatelliteNames {
		t.Run("token registration rejects satellite "+name, func(t *testing.T) {
			w := newWorld()
			seedRegistration(w)
			_, err := w.services(t).Satellites.Register(ctx, fleet.RegisterSatelliteCmd{Name: name, ConfigName: "edge-config"})
			require.ErrorIs(t, err, fleet.ErrInvalid)
			require.Zero(t, w.rec.Count("Satellites.Create"))
		})

		t.Run("SPIFFE ZTR does not create satellite "+name, func(t *testing.T) {
			w := newWorld()
			w.repo.Configs.ByName = map[string]fleet.Config{fleet.DefaultConfigName: {ID: 3, Name: fleet.DefaultConfigName}}
			_, err := w.services(t).Registration.SPIFFEZeroTouch(ctx, name)
			require.ErrorIs(t, err, fleet.ErrInvalid)
			require.Zero(t, w.rec.Count("Satellites.Create"))
		})

		t.Run("SPIFFE registration rejects new satellite "+name, func(t *testing.T) {
			w := newWorld()
			w.identity.Domain = "example.org"
			_, err := w.services(t).Registration.RegisterSPIFFE(ctx, fleet.RegisterSPIFFECmd{Name: name, AttestationMethod: fleet.AttestJoinToken, Selectors: []string{"unix:uid:0"}})
			require.ErrorIs(t, err, fleet.ErrInvalid)
			require.Zero(t, w.rec.Count("Satellites.Create"))
			require.Equal(t, 1, w.rec.Count("DeleteEntry"), "the workload entry is removed again")
		})

		t.Run("SPIFFE registration keeps working for existing satellite "+name, func(t *testing.T) {
			w := newWorld()
			w.identity.Domain = "example.org"
			w.repo.Satellites.ByName = map[string]fleet.Satellite{name: {ID: 4, Name: name}}
			_, err := w.services(t).Registration.RegisterSPIFFE(ctx, fleet.RegisterSPIFFECmd{Name: name, AttestationMethod: fleet.AttestJoinToken, Selectors: []string{"unix:uid:0"}})
			require.NoError(t, err)
		})
	}

	for _, name := range fleet.ReservedGroupNames {
		t.Run("group sync does not create group "+name, func(t *testing.T) {
			w := newWorld()
			_, err := w.services(t).Groups.Sync(ctx, fleet.SyncGroupCmd{Name: name})
			require.ErrorIs(t, err, fleet.ErrInvalid)
			require.Zero(t, w.rec.Count("Groups.Upsert"))
			require.Zero(t, w.rec.Count("Commit"))
		})

		t.Run("group sync keeps updating existing group "+name, func(t *testing.T) {
			w := newWorld()
			w.repo.Groups.ByName = map[string]fleet.Group{name: {ID: 5, Name: name}}
			_, err := w.services(t).Groups.Sync(ctx, fleet.SyncGroupCmd{Name: name})
			require.NoError(t, err)
			require.Equal(t, 1, w.rec.Count("Groups.Upsert"))
		})
	}
}
