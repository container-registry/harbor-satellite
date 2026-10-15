package fleet_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/container-registry/harbor-satellite/internal/groundcontrol/fleet"
	"github.com/container-registry/harbor-satellite/internal/shared/testutil/pending"
	"github.com/stretchr/testify/require"
)

// seedZTR is satellite edge-01 (ID 7) with token "tok", robot 42 and a
// registry that rotates the secret to "fresh".
func seedZTR(w *world) {
	w.repo.Tokens.Claimable = map[string]fleet.SatelliteID{"tok": 7}
	w.repo.Satellites.ByName = map[string]fleet.Satellite{"edge-01": {ID: 7, Name: "edge-01"}}
	w.repo.Robots.BySatelliteID = map[fleet.SatelliteID]fleet.Robot{7: {ID: 5, Satellite: 7, Name: "robot$edge-01", HarborID: 42}}
	w.registry.Credentials = fleet.RobotCredentials{HarborID: 42, Name: "robot$edge-01", Secret: "fresh", ExpiresAt: now.Add(30 * 24 * time.Hour)}
}

func TestRegistrationService_ZeroTouch(t *testing.T) {
	t.Run("claims, rotates, then stores the hash and consumes in one transaction", func(t *testing.T) {
		pending.Plan(t, "2.5", pendingServices)
		w := newWorld()
		seedZTR(w)

		res, err := w.services(t).Registration.ZeroTouch(context.Background(), "tok")

		require.NoError(t, err)
		require.Equal(t, "edge-01", res.SatelliteName)
		require.Equal(t, stateRef("satellite", "edge-01").String(), res.StateURL)
		require.Equal(t, "robot$edge-01", res.Credentials.Name)
		require.Equal(t, "fresh", res.Credentials.Secret)
		claim := onlyCall(t, w.rec, "Tokens.Claim")
		require.Equal(t, "tok", arg[string](t, claim, 0))
		require.Equal(t, now, arg[time.Time](t, claim, 1))
		require.Equal(t, int64(42), arg[int64](t, onlyCall(t, w.rec, "RotateRobotSecret"), 0))
		noRegistryCalls(t, w.rec, "RotateRobotSecret")
		stored := arg[fleet.Robot](t, onlyCall(t, w.rec, "Robots.Update"), 0)
		require.NotEmpty(t, stored.SecretHash)
		require.NotEqual(t, "fresh", stored.SecretHash)
		require.Equal(t, "tok", arg[string](t, onlyCall(t, w.rec, "Tokens.Consume"), 0))
		// A62: neither the claim nor the registry call holds a transaction.
		outsideTx(t, w.rec, "Tokens.Claim")
		outsideTx(t, w.rec, "RotateRobotSecret")
		before(t, w.rec, "Tokens.Claim", "WithTx")
		before(t, w.rec, "RotateRobotSecret", "WithTx")
		insideTx(t, w.rec, "Robots.Update")
		insideTx(t, w.rec, "Tokens.Consume")
		require.Zero(t, w.rec.Count("Tokens.Unclaim"))
	})

	t.Run("no robot yet is ErrNotProvisioned and keeps the token", func(t *testing.T) {
		pending.Plan(t, "2.5", pendingServices)
		w := newWorld()
		seedZTR(w)
		w.repo.Robots.BySatelliteID = nil

		_, err := w.services(t).Registration.ZeroTouch(context.Background(), "tok")

		require.ErrorIs(t, err, fleet.ErrNotProvisioned)
		require.Equal(t, "tok", arg[string](t, onlyCall(t, w.rec, "Tokens.Unclaim"), 0))
		noRegistryCalls(t, w.rec)
	})

	for _, step := range []string{"RotateRobotSecret", "Robots.Update"} {
		t.Run("unclaims the token when "+step+" fails", func(t *testing.T) {
			pending.Plan(t, "2.5", pendingServices)
			w := newWorld()
			seedZTR(w)
			boom := errors.New("step failed")
			w.rec.FailOn(step, boom)

			_, err := w.services(t).Registration.ZeroTouch(context.Background(), "tok")

			require.ErrorIs(t, err, boom)
			require.Equal(t, "tok", arg[string](t, onlyCall(t, w.rec, "Tokens.Unclaim"), 0))
			require.Zero(t, w.rec.Count("Tokens.Consume"))
		})
	}

	t.Run("expired token is ErrTokenExpired", func(t *testing.T) {
		pending.Plan(t, "2.5", pendingServices)
		w := newWorld()
		seedZTR(w)
		w.rec.FailOn("Tokens.Claim", fleet.ErrTokenExpired)

		_, err := w.services(t).Registration.ZeroTouch(context.Background(), "tok")

		require.ErrorIs(t, err, fleet.ErrTokenExpired)
		require.Zero(t, w.rec.Count("RotateRobotSecret"))
	})

	t.Run("unknown or claimed token is ErrTokenInvalid", func(t *testing.T) {
		pending.Plan(t, "2.5", pendingServices)
		w := newWorld()
		seedZTR(w)

		_, err := w.services(t).Registration.ZeroTouch(context.Background(), "already-claimed")

		require.ErrorIs(t, err, fleet.ErrTokenInvalid)
		require.Zero(t, w.rec.Count("RotateRobotSecret"))
		require.Zero(t, w.rec.Count("Tokens.Unclaim"))
	})
}

func TestRegistrationService_SPIFFEZeroTouch(t *testing.T) {
	t.Run("an unknown satellite is created with the default config and waits for its robot", func(t *testing.T) {
		pending.Plan(t, "2.5", pendingServices)
		w := newWorld()
		w.repo.Satellites.NextID = 9
		w.repo.Configs.ByName = map[string]fleet.Config{fleet.DefaultConfigName: {ID: 3, Name: fleet.DefaultConfigName}}

		_, err := w.services(t).Registration.SPIFFEZeroTouch(context.Background(), "edge-09")

		require.ErrorIs(t, err, fleet.ErrNotProvisioned)
		require.Equal(t, "edge-09", arg[string](t, onlyCall(t, w.rec, "Satellites.Create"), 0))
		set := onlyCall(t, w.rec, "Satellites.SetConfig")
		require.Equal(t, fleet.SatelliteID(9), arg[fleet.SatelliteID](t, set, 0))
		require.Equal(t, fleet.ConfigID(3), arg[fleet.ConfigID](t, set, 1))
		require.Equal(t, []fleet.Event{{
			Kind:      fleet.EventSatelliteCreated,
			Aggregate: fleet.Aggregate{Type: fleet.AggregateSatellite, ID: 9},
			Payload:   fleet.ChangedPayload{Name: "edge-09"},
		}}, w.repo.Events.Appended())
		require.Equal(t, 1, w.rec.Count("Commit"))
		noRegistryCalls(t, w.rec)
	})

	t.Run("creates the default config when it is missing", func(t *testing.T) {
		pending.Plan(t, "2.5", pendingServices)
		w := newWorld()
		w.repo.Satellites.NextID = 9
		w.repo.Configs.NextID = 4

		_, err := w.services(t).Registration.SPIFFEZeroTouch(context.Background(), "edge-09")

		require.ErrorIs(t, err, fleet.ErrNotProvisioned)
		created := arg[fleet.Config](t, onlyCall(t, w.rec, "Configs.Create"), 0)
		require.Equal(t, fleet.DefaultConfigName, created.Name)
		require.Equal(t, fleet.ConfigID(4), arg[fleet.ConfigID](t, onlyCall(t, w.rec, "Satellites.SetConfig"), 1))
		require.Contains(t, w.repo.Events.Appended(), configChanged(4, fleet.DefaultConfigName))
	})

	t.Run("a provisioned satellite gets a fresh secret", func(t *testing.T) {
		pending.Plan(t, "2.5", pendingServices)
		w := newWorld()
		seedZTR(w)

		res, err := w.services(t).Registration.SPIFFEZeroTouch(context.Background(), "edge-01")

		require.NoError(t, err)
		require.Equal(t, "fresh", res.Credentials.Secret)
		require.Equal(t, stateRef("satellite", "edge-01").String(), res.StateURL)
		noRegistryCalls(t, w.rec, "RotateRobotSecret")
		outsideTx(t, w.rec, "RotateRobotSecret")
		require.Zero(t, w.rec.Count("Satellites.Create"))
	})
}

// D2: the refresh returns the new secret, so it rotates in the request.
func TestRegistrationService_RefreshCredentialsRotatesAndStoresHash(t *testing.T) {
	pending.Plan(t, "2.5", pendingServices)
	w := newWorld()
	seedZTR(w)

	creds, err := w.services(t).Registration.RefreshCredentials(context.Background(), "edge-01")

	require.NoError(t, err)
	require.Equal(t, "fresh", creds.Secret)
	require.Equal(t, int64(42), arg[int64](t, onlyCall(t, w.rec, "RotateRobotSecret"), 0))
	noRegistryCalls(t, w.rec, "RotateRobotSecret")
	stored := arg[fleet.Robot](t, onlyCall(t, w.rec, "Robots.Update"), 0)
	require.Equal(t, fleet.RobotID(5), stored.ID)
	require.NotEqual(t, "fresh", stored.SecretHash)
}

func TestNewServices_RequiresRepositoryAndRegistry(t *testing.T) {
	pending.Plan(t, "1.2", "fleet services are stubs")
	w := newWorld()

	_, err := fleet.NewServices(fleet.Deps{Registry: w.registry})
	require.ErrorIs(t, err, fleet.ErrInvalid)
	_, err = fleet.NewServices(fleet.Deps{Repository: w.repo})
	require.ErrorIs(t, err, fleet.ErrInvalid)
	svc, err := fleet.NewServices(fleet.Deps{Repository: w.repo, Registry: w.registry})
	require.NoError(t, err)
	require.NotNil(t, svc.Satellites)
	require.NotNil(t, svc.Groups)
	require.NotNil(t, svc.Configs)
	require.NotNil(t, svc.Registration)
}
