package registryauth_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/container-registry/harbor-satellite/internal/satellite/registryauth"
	"github.com/container-registry/harbor-satellite/internal/satellite/registryauth/registryauthtest"
	"github.com/container-registry/harbor-satellite/internal/shared/testutil/pending"
	"github.com/stretchr/testify/require"
)

const pendingKeyless = "the satellite pulls with its robot only; there is no keyless pull"

var keylessNow = time.Date(2026, 10, 15, 12, 0, 0, 0, time.UTC)

func TestStaticProvider_ReturnsCurrentCredentials(t *testing.T) {
	username, password := "robot$edge-01-7", "first"
	p := registryauth.StaticProvider{Credentials: func() (string, string) { return username, password }}

	first, err := p.Credential(context.Background(), "harbor.example")
	require.NoError(t, err)
	username, password = "robot$edge-01-7-g1", "second"
	second, err := p.Credential(context.Background(), "harbor.example")
	require.NoError(t, err)

	require.Equal(t, registryauth.Credential{Username: "robot$edge-01-7", Password: "first"}, first)
	require.Equal(t, registryauth.Credential{Username: "robot$edge-01-7-g1", Password: "second"}, second, "a rotation applies without a new provider")
}

func newWorkloadIdentity(t *testing.T, now *time.Time) (*registryauth.WorkloadIdentityProvider, *registryauthtest.JWTSVIDSource, *registryauthtest.TokenExchanger) {
	t.Helper()
	source := &registryauthtest.JWTSVIDSource{SVID: "jwt-svid"}
	exchanger := &registryauthtest.TokenExchanger{Credential: registryauth.Credential{BearerToken: "pull-token", ExpiresAt: keylessNow.Add(time.Hour)}}
	p, err := registryauth.NewWorkloadIdentityProvider(source, exchanger, "harbor.example", func() time.Time { return *now })
	require.NoError(t, err)
	return p, source, exchanger
}

func TestWorkloadIdentityProvider_ExchangesJWTSVID(t *testing.T) {
	pending.Plan(t, "8.3", pendingKeyless)
	now := keylessNow
	p, source, exchanger := newWorkloadIdentity(t, &now)

	cred, err := p.Credential(context.Background(), "harbor.example")

	require.NoError(t, err)
	require.Equal(t, "pull-token", cred.BearerToken)
	require.Empty(t, cred.Password)
	require.Equal(t, 1, source.Count("FetchJWTSVID"))
	require.Equal(t, 1, exchanger.Count("Exchange"))
}

func TestWorkloadIdentityProvider_CachesUntilExpiry(t *testing.T) {
	pending.Plan(t, "8.3", pendingKeyless)
	now := keylessNow
	p, _, exchanger := newWorkloadIdentity(t, &now)

	for range 3 {
		_, err := p.Credential(context.Background(), "harbor.example")
		require.NoError(t, err)
	}
	require.Equal(t, 1, exchanger.Count("Exchange"), "cached while valid")

	now = keylessNow.Add(time.Hour)
	_, err := p.Credential(context.Background(), "harbor.example")
	require.NoError(t, err)
	require.Equal(t, 2, exchanger.Count("Exchange"), "an expired token is exchanged again")
}

func TestWorkloadIdentityProvider_SourceFailureIsError(t *testing.T) {
	pending.Plan(t, "8.3", pendingKeyless)
	now := keylessNow
	p, source, exchanger := newWorkloadIdentity(t, &now)
	boom := errors.New("workload API unavailable")
	source.FailOn("FetchJWTSVID", boom)

	_, err := p.Credential(context.Background(), "harbor.example")

	require.ErrorIs(t, err, boom)
	require.Zero(t, exchanger.Count("Exchange"))
}
